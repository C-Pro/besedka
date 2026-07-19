package backup

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// IncrementalOnce uploads the changes since the previous backup as an
// incremental artifact chained onto it, then flushes pending attachment
// uploads and prunes. When the chain cannot be extended safely it promotes to
// a full backup instead (see chainParent).
func (s *Scheduler) IncrementalOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.incremental(ctx, false)
}

// FinalBackup is the shutdown backup. It is incremental whenever a healthy
// chain exists — small enough to fit a spot instance's termination grace
// period — and a full backup otherwise (in particular when no backup was ever
// made). Unlike scheduled backups, a failure to flush pending attachment
// blobs is returned rather than logged: after shutdown there is no next
// backfill, so degrading silently would lose the blobs for good. The database
// artifact is still uploaded first, so a flush failure never risks messages.
func (s *Scheduler) FinalBackup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.incremental(ctx, true)
}

// incremental performs one incremental backup (promoting to full when the
// chain cannot be extended) followed by the shared post-upload steps. Callers
// must hold s.mu.
func (s *Scheduler) incremental(ctx context.Context, flushFatal bool) error {
	parent, reason := s.chainParent(ctx)
	if parent == "" {
		slog.Info("taking a full backup instead of incremental", "reason", reason)
		if err := s.fullBackup(ctx); err != nil {
			return err
		}
	} else if err := s.incrementalBackup(ctx, parent); err != nil {
		return err
	}
	return s.finish(ctx, flushFatal)
}

// chainParent decides whether an incremental backup can safely extend the
// existing chain. It returns the parent artifact key when it can, or "" and a
// reason when a full backup is required. The chain is extendable only when
// the newest artifact in object storage is exactly the one this database
// uploaded last: any divergence — first run, fresh upgrade, restored from
// backup, a failed chain-state commit, another writer on the same prefix, or
// manual deletion — degrades to a full backup, which re-roots the chain.
func (s *Scheduler) chainParent(ctx context.Context) (parent, reason string) {
	objs, err := s.obj.List(ctx, s.prefix)
	if err != nil {
		return "", fmt.Sprintf("cannot list existing backups: %v", err)
	}
	keys := make([]string, 0, len(objs))
	hasFull := false
	for _, o := range objs {
		keys = append(keys, o.Key)
		hasFull = hasFull || isFullKey(o.Key)
	}
	if !hasFull {
		return "", "no full backup in object storage"
	}
	sort.Strings(keys)
	newest := keys[len(keys)-1]

	lastKey, _, err := s.store.BackupState()
	if err != nil {
		return "", fmt.Sprintf("cannot read local backup state: %v", err)
	}
	if lastKey == "" {
		return "", "no backup committed by this database yet"
	}
	if newest != lastKey {
		return "", fmt.Sprintf("chain diverged: newest artifact is %s, last committed here is %s", newest, lastKey)
	}
	return lastKey, ""
}

// incrementalBackup captures, encrypts, and uploads the delta since parent,
// then commits the chain state. When nothing changed since the last backup it
// uploads nothing: the parent already captures the current state, so an empty
// artifact would only add clutter and an S3 request. Attachment flushing still
// happens afterwards (see incremental), so a clean shutdown with no message
// changes but pending blobs still pushes those blobs. Callers must hold s.mu.
func (s *Scheduler) incrementalBackup(ctx context.Context, parent string) error {
	payload, txid, count, err := s.store.IncrementalSnapshot()
	if err != nil {
		return fmt.Errorf("incremental snapshot failed: %w", err)
	}
	if count == 0 {
		slog.Info("no changes since last backup, skipping incremental", "parent", parent)
		return nil
	}
	enc, salt, err := s.store.EncryptBackup(payload)
	if err != nil {
		return fmt.Errorf("backup encryption failed: %w", err)
	}

	var artifact bytes.Buffer
	h := header{version: headerVersion2, kind: kindIncremental, salt: salt, parent: parent}
	if err := writeHeader(&artifact, h, enc); err != nil {
		return fmt.Errorf("failed to assemble backup artifact: %w", err)
	}

	ts := s.now().UTC()
	key := s.prefix + "besedka-" + ts.Format("20060102T150405Z") + incrSuffix
	for key <= parent {
		// Same-second as the parent (e.g. shutdown racing the ticker): reusing
		// the key would overwrite the parent and break the chain.
		ts = ts.Add(time.Second)
		key = s.prefix + "besedka-" + ts.Format("20060102T150405Z") + incrSuffix
	}
	data := artifact.Bytes()
	if err := s.obj.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		return fmt.Errorf("backup upload failed: %w", err)
	}
	slog.Info("incremental database backup uploaded", "key", key, "parent", parent, "bytes", len(data))

	s.commitChain(key, txid)
	return nil
}
