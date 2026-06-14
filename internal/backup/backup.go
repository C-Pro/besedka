// Package backup snapshots the Besedka database to S3-compatible object storage
// on a schedule and restores it on startup when the local database is missing.
package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"besedka/internal/objectstore"
)

// Snapshotter is the subset of storage.BboltStorage the scheduler needs.
type Snapshotter interface {
	// SnapshotTo writes a consistent database snapshot to w.
	SnapshotTo(w io.Writer) (int64, error)
	// EncryptForBackup encrypts a payload when at-rest encryption is enabled,
	// returning the (possibly encrypted) bytes, the salt to reconstruct the key,
	// and whether encryption was applied.
	EncryptForBackup(data []byte) (out []byte, salt []byte, ok bool, err error)
}

// Scheduler periodically uploads database snapshots to object storage.
type Scheduler struct {
	store    Snapshotter
	obj      *objectstore.Client
	prefix   string
	interval time.Duration
	keep     int
	now      func() time.Time
}

// NewScheduler builds a Scheduler. prefix is the object-key prefix for backups
// (e.g. "backups/"); keep is the number of most-recent backups to retain.
func NewScheduler(store Snapshotter, obj *objectstore.Client, prefix string, interval time.Duration, keep int) *Scheduler {
	if keep < 1 {
		keep = 1
	}
	return &Scheduler{
		store:    store,
		obj:      obj,
		prefix:   prefix,
		interval: interval,
		keep:     keep,
		now:      time.Now,
	}
}

// Run performs a backup every interval until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.BackupOnce(ctx); err != nil {
				slog.Error("database backup failed", "error", err)
			}
		}
	}
}

// BackupOnce takes a snapshot, encrypts it if enabled, uploads it under a
// timestamped key, and prunes old backups beyond the retention count.
func (s *Scheduler) BackupOnce(ctx context.Context) error {
	var snap bytes.Buffer
	if _, err := s.store.SnapshotTo(&snap); err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	payload, salt, encrypted, err := s.store.EncryptForBackup(snap.Bytes())
	if err != nil {
		return fmt.Errorf("backup encryption failed: %w", err)
	}

	var artifact bytes.Buffer
	if err := writeHeader(&artifact, header{encrypted: encrypted, salt: salt}, payload); err != nil {
		return fmt.Errorf("failed to assemble backup artifact: %w", err)
	}

	key := s.prefix + "besedka-" + s.now().UTC().Format("20060102T150405Z") + ".bak"
	data := artifact.Bytes()
	if err := s.obj.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		return fmt.Errorf("backup upload failed: %w", err)
	}
	slog.Info("database backup uploaded", "key", key, "bytes", len(data), "encrypted", encrypted)

	if err := s.prune(ctx); err != nil {
		// Pruning failure is non-fatal; the backup itself succeeded.
		slog.Error("backup retention prune failed", "error", err)
	}
	return nil
}

// prune deletes all but the newest s.keep backups under the prefix.
func (s *Scheduler) prune(ctx context.Context) error {
	objs, err := s.obj.List(ctx, s.prefix)
	if err != nil {
		return err
	}
	if len(objs) <= s.keep {
		return nil
	}
	keys := make([]string, len(objs))
	for i, o := range objs {
		keys[i] = o.Key
	}
	sort.Strings(keys) // timestamped keys sort chronologically; newest last.

	for _, key := range keys[:len(keys)-s.keep] {
		if err := s.obj.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to delete old backup %s: %w", key, err)
		}
		slog.Info("pruned old backup", "key", key)
	}
	return nil
}
