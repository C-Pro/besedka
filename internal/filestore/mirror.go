package filestore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"besedka/internal/objectstore"
)

const (
	mirrorWorkers   = 4
	mirrorQueueSize = 256
	// mirrorGetTimeout bounds an on-demand download triggered by a local miss.
	mirrorGetTimeout = 60 * time.Second
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

// Start launches the upload worker pool and a one-time backfill of existing
// local files, then blocks until ctx is cancelled and the workers exit. It is
// intended to run inside the application's errgroup.
func (m *MirrorFileStore) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < mirrorWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.worker(ctx)
		}()
	}
	go m.backfill(ctx)
	wg.Wait()
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
// copy still exists and startup backfill will re-upload it later.
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

	rc, err := m.local.Get(hash)
	if err != nil {
		slog.Error("mirror: failed to read local blob for upload", "hash", hash, "error", err)
		return
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		slog.Error("mirror: failed to read local blob for upload", "hash", hash, "error", err)
		return
	}

	if err := m.obj.Put(ctx, m.prefix+hash, bytes.NewReader(data), int64(len(data))); err != nil {
		if ctx.Err() == nil {
			slog.Error("mirror: failed to upload blob to object storage", "hash", hash, "error", err)
		}
	}
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
