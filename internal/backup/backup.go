// Package backup snapshots the Besedka database to S3-compatible object storage
// on a schedule and restores it on startup when the local database is missing.
//
// Two artifact kinds exist: full snapshots (the whole database, uploaded every
// interval) and incremental snapshots (only records changed since the previous
// artifact, uploaded every incrInterval and on shutdown). Incrementals form a
// chain rooted at the newest full; recovery replays the chain in order.
package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"besedka/internal/objectstore"
)

const (
	fullSuffix = "-full.bak"
	incrSuffix = "-incr.bak"
)

// isFullKey reports whether an object key holds a full snapshot. Keys from
// before incremental backups existed ("...Z.bak") are full snapshots too.
func isFullKey(key string) bool {
	return strings.HasSuffix(key, fullSuffix) ||
		(strings.HasSuffix(key, ".bak") && !strings.HasSuffix(key, incrSuffix))
}

// Store is the subset of storage.BboltStorage the scheduler needs.
type Store interface {
	// SnapshotToWithTxID writes a consistent database snapshot to w and
	// reports the transaction id it covers.
	SnapshotToWithTxID(w io.Writer) (int64, uint64, error)
	// EncryptBackup encrypts a payload with the data-at-rest key, returning the
	// ciphertext and the salt needed to derive the key on recovery.
	EncryptBackup(data []byte) (ciphertext []byte, salt []byte, err error)
	// IncrementalSnapshot serializes all changes since the last CommitBackup
	// and reports how many changed entries it holds (0 when nothing changed).
	IncrementalSnapshot() (payload []byte, txid uint64, count int, err error)
	// CommitBackup records a successful upload: clears covered dirty markers
	// and persists the chain state.
	CommitBackup(lastKey string, txid uint64) error
	// BackupState returns the last committed chain state ("" if none).
	BackupState() (lastKey string, txid uint64, err error)
}

// Scheduler periodically uploads database snapshots to object storage.
type Scheduler struct {
	store        Store
	obj          *objectstore.Client
	prefix       string
	interval     time.Duration
	incrInterval time.Duration
	keep         int
	flush        func(context.Context) error
	now          func() time.Time

	// mu serializes backups: the two tickers, shutdown, and the on-demand
	// endpoint must not snapshot and commit chain state concurrently.
	mu sync.Mutex
}

// NewScheduler builds a Scheduler. prefix is the object-key prefix for backups
// (e.g. "backups/"); interval and incrInterval are the full and incremental
// cadences (incrInterval 0 disables incrementals); keep is the number of
// most-recent full backups (with their incrementals) to retain. flush, if
// non-nil, is called after every successful upload to push not-yet-mirrored
// attachment blobs to object storage — after, because messages matter more
// than attachments.
func NewScheduler(store Store, obj *objectstore.Client, prefix string, interval, incrInterval time.Duration, keep int, flush func(context.Context) error) *Scheduler {
	if keep < 1 {
		keep = 1
	}
	return &Scheduler{
		store:        store,
		obj:          obj,
		prefix:       prefix,
		interval:     interval,
		incrInterval: incrInterval,
		keep:         keep,
		flush:        flush,
		now:          time.Now,
	}
}

// Run performs backups on both cadences until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	full := time.NewTicker(s.interval)
	defer full.Stop()

	var incrC <-chan time.Time
	if s.incrInterval > 0 {
		incr := time.NewTicker(s.incrInterval)
		defer incr.Stop()
		incrC = incr.C
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-full.C:
			if err := s.DoBackup(ctx); err != nil {
				slog.Error("database backup failed", "error", err)
			}
		case <-incrC:
			if err := s.DoIncrementalBackup(ctx); err != nil {
				slog.Error("incremental database backup failed", "error", err)
			}
		}
	}
}

// DoBackup takes a full snapshot, encrypts it, uploads it under a
// timestamped key, then flushes pending attachment uploads and prunes old
// backups beyond the retention count.
func (s *Scheduler) DoBackup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fullBackup(ctx); err != nil {
		return err
	}
	return s.finish(ctx, false)
}

// fullBackup uploads a full snapshot and commits the chain state. Full
// artifacts keep the version-1 header so binaries that predate incremental
// backups can still restore them. Callers must hold s.mu.
func (s *Scheduler) fullBackup(ctx context.Context) error {
	payload, salt, txid, err := s.encryptedSnapshot()
	if err != nil {
		return err
	}

	var artifact bytes.Buffer
	if err := writeHeader(&artifact, header{salt: salt}, payload); err != nil {
		return fmt.Errorf("failed to assemble backup artifact: %w", err)
	}

	lastKey, _, _ := s.store.BackupState()
	ts := s.now().UTC()
	key := s.prefix + "besedka-" + ts.Format("20060102T150405Z") + fullSuffix
	for key <= lastKey {
		ts = ts.Add(time.Second)
		key = s.prefix + "besedka-" + ts.Format("20060102T150405Z") + fullSuffix
	}
	data := artifact.Bytes()
	if err := s.obj.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		return fmt.Errorf("backup upload failed: %w", err)
	}
	slog.Info("database backup uploaded", "key", key, "bytes", len(data))

	s.commitChain(key, txid)
	return nil
}

// commitChain records a successful upload in the database. Failure is logged,
// not returned: the artifact is already safe in object storage, and a stale
// chain state only means the next incremental self-heals by promoting to full.
func (s *Scheduler) commitChain(key string, txid uint64) {
	if err := s.store.CommitBackup(key, txid); err != nil {
		slog.Error("failed to commit backup chain state; next incremental will be a full backup", "key", key, "error", err)
	}
}

// finish runs the post-upload steps shared by all backup kinds: flush pending
// attachment blobs (the DB artifact is already uploaded, keeping messages
// ahead of attachments), then prune. flushFatal makes a flush failure fatal —
// used on shutdown, where degrading silently would lose the blobs for good.
// Pruning failure is always non-fatal; the backup itself succeeded.
func (s *Scheduler) finish(ctx context.Context, flushFatal bool) error {
	if s.flush != nil {
		if err := s.flush(ctx); err != nil {
			if flushFatal {
				return fmt.Errorf("attachment flush after backup failed: %w", err)
			}
			slog.Error("attachment flush after backup failed", "error", err)
		}
	}
	if err := s.prune(ctx); err != nil {
		slog.Error("backup retention prune failed", "error", err)
	}
	return nil
}

// encryptedSnapshot snapshots the database and encrypts it. The plaintext
// buffer goes out of scope on return, keeping peak memory near 2x the DB size.
func (s *Scheduler) encryptedSnapshot() (payload, salt []byte, txid uint64, err error) {
	var snap bytes.Buffer
	if _, txid, err = s.store.SnapshotToWithTxID(&snap); err != nil {
		return nil, nil, 0, fmt.Errorf("snapshot failed: %w", err)
	}
	payload, salt, err = s.store.EncryptBackup(snap.Bytes())
	if err != nil {
		return nil, nil, 0, fmt.Errorf("backup encryption failed: %w", err)
	}
	return payload, salt, txid, nil
}

// prune retains the s.keep newest full backups and only the incremental backups
// that chain off the single newest full backup. Incremental backups for older
// full backups are deleted, as are full backups beyond s.keep.
func (s *Scheduler) prune(ctx context.Context) error {
	objs, err := s.obj.List(ctx, s.prefix)
	if err != nil {
		return err
	}
	keys := make([]string, len(objs))
	for i, o := range objs {
		keys[i] = o.Key
	}
	sort.Strings(keys) // timestamped keys sort chronologically; newest last.

	var fullIdx []int
	for i, k := range keys {
		if isFullKey(k) {
			fullIdx = append(fullIdx, i)
		}
	}
	if len(fullIdx) == 0 {
		return nil
	}

	retainedFullCutoff := 0
	if len(fullIdx) > s.keep {
		retainedFullCutoff = fullIdx[len(fullIdx)-s.keep]
	}
	latestFullIdx := fullIdx[len(fullIdx)-1]

	for i, key := range keys {
		shouldDelete := false
		if isFullKey(key) {
			if i < retainedFullCutoff {
				shouldDelete = true
			}
		} else {
			if i < latestFullIdx {
				shouldDelete = true
			}
		}

		if shouldDelete {
			if err := s.obj.Delete(ctx, key); err != nil {
				return fmt.Errorf("failed to delete old backup %s: %w", key, err)
			}
			slog.Info("pruned old backup", "key", key)
		}
	}
	return nil
}
