package filestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"besedka/internal/objectstore"

	"golang.org/x/sync/errgroup"
)

const (
	mirrorWorkers   = 4
	mirrorQueueSize = 256
	// mirrorGetTimeout bounds an on-demand download triggered by a local miss.
	mirrorGetTimeout = 60 * time.Second
	// backfillInterval is how often the backfill re-scans local files, which
	// also retries uploads dropped earlier because the queue was full.
	backfillInterval = time.Hour
)

// MirrorFileStore decorates a local FileStore with an S3-compatible object
// store. Writes go to local disk first (fast, authoritative) and are uploaded
// to object storage asynchronously by a worker pool. Reads are served from
// local disk; on a local miss the blob is downloaded from object storage,
// cached back to local disk, and then served. This makes object storage a
// durable backup and a self-healing cache for the uploads directory.
//
// Encryption is handled by the layer above (storage.SaveFileBlob encrypts
// before Save), so the mirror only ever sees already-encrypted bytes when
// encryption is enabled — it does no crypto itself.
type MirrorFileStore struct {
	local  FileStore
	obj    *objectstore.Client
	prefix string

	queue chan string

	mu       sync.Mutex
	inflight map[string]struct{}
}

// hashWalker is implemented by local stores that can enumerate their blobs,
// enabling startup backfill of pre-existing files.
type hashWalker interface {
	Walk(func(hash string) error) error
}

// NewMirrorFileStore wraps local with object-storage mirroring under keyPrefix
// (e.g. "files/"). Call Start to launch the upload workers and backfill.
func NewMirrorFileStore(local FileStore, obj *objectstore.Client, keyPrefix string) *MirrorFileStore {
	return &MirrorFileStore{
		local:    local,
		obj:      obj,
		prefix:   keyPrefix,
		queue:    make(chan string, mirrorQueueSize),
		inflight: make(map[string]struct{}),
	}
}

// Save writes locally then schedules an asynchronous upload.
func (m *MirrorFileStore) Save(r io.Reader, hash string) error {
	if err := m.local.Save(r, hash); err != nil {
		return err
	}
	m.enqueue(hash)
	return nil
}

// Replace force-writes locally then schedules an asynchronous upload.
func (m *MirrorFileStore) Replace(r io.Reader, hash string) error {
	if err := m.local.Replace(r, hash); err != nil {
		return err
	}
	m.enqueue(hash)
	return nil
}

// Get serves from local disk, falling back to object storage on a miss. A blob
// fetched from object storage is cached back to local disk before being served.
func (m *MirrorFileStore) Get(hash string) (io.ReadCloser, error) {
	rc, err := m.local.Get(hash)
	if err == nil {
		return rc, nil
	}
	localErr := err

	ctx, cancel := context.WithTimeout(context.Background(), mirrorGetTimeout)
	defer cancel()

	remote, rerr := m.obj.Get(ctx, m.prefix+hash)
	if rerr != nil {
		// Not in object storage either: surface the original local error so
		// callers map it to 404 exactly as before.
		return nil, localErr
	}
	defer func() { _ = remote.Close() }()

	if err := m.local.Replace(remote, hash); err != nil {
		return nil, fmt.Errorf("mirror: failed to cache blob %s from object storage: %w", hash, err)
	}
	slog.Info("recovered file from object storage", "hash", hash)
	return m.local.Get(hash)
}

// Start launches the upload worker pool and the periodic backfill of local
// files missing from object storage, then blocks until ctx is cancelled and
// the workers exit. It is intended to run inside the application's errgroup.
func (m *MirrorFileStore) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < mirrorWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.worker(ctx)
		}()
	}
	go m.backfillLoop(ctx)
	wg.Wait()
}

// backfillLoop backfills once at startup and then every backfillInterval, so
// uploads dropped from a full queue are retried without waiting for a restart.
func (m *MirrorFileStore) backfillLoop(ctx context.Context) {
	m.backfill(ctx)
	ticker := time.NewTicker(backfillInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.backfill(ctx)
		}
	}
}

func (m *MirrorFileStore) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case hash := <-m.queue:
			m.upload(ctx, hash)
		}
	}
}

// enqueue schedules a non-blocking upload from the request hot path. It dedups
// in-flight hashes and drops (with a warning) if the queue is full — the local
// copy still exists and the next periodic backfill will re-upload it.
func (m *MirrorFileStore) enqueue(hash string) {
	if !m.markInflight(hash) {
		return
	}
	select {
	case m.queue <- hash:
	default:
		m.clearInflight(hash)
		slog.Warn("mirror upload queue full, dropping (local copy retained)", "hash", hash)
	}
}

func (m *MirrorFileStore) upload(ctx context.Context, hash string) {
	m.clearInflight(hash)

	if err := m.uploadHash(ctx, hash); err != nil {
		if ctx.Err() == nil {
			slog.Error("mirror: failed to upload blob to object storage", "hash", hash, "error", err)
		}
	}
}

// uploadHash reads a local blob and uploads it to object storage.
func (m *MirrorFileStore) uploadHash(ctx context.Context, hash string) error {
	rc, err := m.local.Get(hash)
	if err != nil {
		return fmt.Errorf("read local blob: %w", err)
	}
	defer func() { _ = rc.Close() }()

	body, size, err := uploadBody(rc)
	if err != nil {
		return fmt.Errorf("read local blob: %w", err)
	}

	return m.obj.Put(ctx, m.prefix+hash, body, size)
}

// Flush synchronously uploads every local blob missing from object storage and
// reports any failure, unlike the fire-and-forget queue and backfill. Backups
// call it after the database artifact is uploaded, so messages are never less
// durable than the attachments they reference. Racing the background workers
// is harmless: blobs are content-addressed, so a duplicate upload writes the
// same bytes to the same key.
func (m *MirrorFileStore) Flush(ctx context.Context) error {
	walker, ok := m.local.(hashWalker)
	if !ok {
		return nil
	}

	objs, err := m.obj.List(ctx, m.prefix)
	if err != nil {
		return fmt.Errorf("mirror flush: failed to list object storage: %w", err)
	}
	existing := make(map[string]struct{}, len(objs))
	for _, o := range objs {
		existing[strings.TrimPrefix(o.Key, m.prefix)] = struct{}{}
	}

	var missing []string
	if err := walker.Walk(func(hash string) error {
		if _, ok := existing[hash]; !ok {
			missing = append(missing, hash)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("mirror flush: failed to walk local files: %w", err)
	}
	if len(missing) == 0 {
		return nil
	}

	var (
		g    errgroup.Group
		mu   sync.Mutex
		errs []error
	)
	g.SetLimit(mirrorWorkers)
	for _, hash := range missing {
		g.Go(func() error {
			if err := m.uploadHash(ctx, hash); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("blob %s: %w", hash, err))
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("mirror flush: %w", errors.Join(errs...))
	}
	slog.Info("mirror flush uploaded pending files", "count", len(missing))
	return nil
}

// uploadBody returns r and its size directly when r is seekable (a local
// file), letting Put stream it without buffering the blob in memory; otherwise
// it falls back to reading r fully.
func uploadBody(r io.Reader) (io.Reader, int64, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		size, err := rs.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, 0, err
		}
		return rs, size, nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

// backfill enqueues every local blob not already present in object storage.
func (m *MirrorFileStore) backfill(ctx context.Context) {
	walker, ok := m.local.(hashWalker)
	if !ok {
		return
	}

	objs, err := m.obj.List(ctx, m.prefix)
	if err != nil {
		// Abort rather than risk re-uploading everything on a transient error.
		slog.Error("mirror backfill: failed to list object storage, skipping", "error", err)
		return
	}
	existing := make(map[string]struct{}, len(objs))
	for _, o := range objs {
		existing[strings.TrimPrefix(o.Key, m.prefix)] = struct{}{}
	}

	count := 0
	_ = walker.Walk(func(hash string) error {
		if _, ok := existing[hash]; ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m.queue <- hash:
			count++
		}
		return nil
	})
	if count > 0 {
		slog.Info("mirror backfill enqueued existing files for upload", "count", count)
	}
}

// markInflight records hash as queued; it returns false if already queued.
func (m *MirrorFileStore) markInflight(hash string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.inflight[hash]; ok {
		return false
	}
	m.inflight[hash] = struct{}{}
	return true
}

func (m *MirrorFileStore) clearInflight(hash string) {
	m.mu.Lock()
	delete(m.inflight, hash)
	m.mu.Unlock()
}
