package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"besedka/internal/objectstore"
	"besedka/internal/storage"

	"go.etcd.io/bbolt"
)

// RecoverDBIfMissing restores the database file from object storage when
// dbPath does not exist locally: the newest full snapshot, then every
// incremental chained after it, applied in order. It is a no-op (returns
// false) when object storage is disabled, the database already exists, or no
// backups are present. The secret is the AUTH_SECRET used to derive the
// decryption keys; each artifact carries its own salt in its self-describing
// header.
//
// It must run before the database is opened. A decryption failure or a broken
// incremental chain is fatal (returns an error) rather than silently starting
// with an empty or stale database. The database is assembled at a temporary
// path and renamed into place only when the whole chain applied cleanly.
func RecoverDBIfMissing(ctx context.Context, dbPath, secret, prefix string, obj *objectstore.Client) (recovered bool, err error) {
	if obj == nil {
		return false, nil
	}
	if _, err := os.Stat(dbPath); err == nil {
		return false, nil // never overwrite an existing database
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to stat %s: %w", dbPath, err)
	}

	objs, err := obj.List(ctx, prefix)
	if err != nil {
		return false, fmt.Errorf("failed to list backups: %w", err)
	}
	if len(objs) == 0 {
		return false, nil // fresh install: nothing to recover
	}

	keys := make([]string, len(objs))
	for i, o := range objs {
		keys[i] = o.Key
	}
	sort.Strings(keys) // timestamped keys: newest sorts last

	newestFull := -1
	for i := len(keys) - 1; i >= 0; i-- {
		if isFullKey(keys[i]) {
			newestFull = i
			break
		}
	}
	if newestFull == -1 {
		return false, fmt.Errorf("found %d backups but no full snapshot; cannot recover", len(keys))
	}
	chain := keys[newestFull:]

	tmpPath := dbPath + ".recover"
	defer func() {
		if !recovered {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := restoreFull(ctx, obj, chain[0], secret, tmpPath); err != nil {
		return false, err
	}
	if len(chain) > 1 {
		if err := applyChain(ctx, obj, chain, secret, tmpPath); err != nil {
			return false, err
		}
	}

	if err := os.Rename(tmpPath, dbPath); err != nil {
		return false, fmt.Errorf("failed to move recovered database into place: %w", err)
	}
	return true, nil
}

// restoreFull downloads and decrypts the full snapshot at key into path.
func restoreFull(ctx context.Context, obj *objectstore.Client, key, secret, path string) error {
	hdr, payload, err := fetchArtifact(ctx, obj, key)
	if err != nil {
		return err
	}
	if hdr.kind != kindFull {
		return fmt.Errorf("backup %s is not a full snapshot", key)
	}

	dbBytes, err := decryptArtifact(key, secret, hdr, payload)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, dbBytes, 0600); err != nil {
		return fmt.Errorf("failed to write recovered database: %w", err)
	}
	return nil
}

// applyChain opens the restored database at path and applies each incremental
// in chain (chain[0] is the full snapshot the rest hang off). Every artifact
// must name its predecessor as parent: a gap in the chain — for example a
// manually deleted object — aborts recovery instead of silently restoring a
// prefix of history.
func applyChain(ctx context.Context, obj *objectstore.Client, chain []string, secret, path string) error {
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return fmt.Errorf("failed to open recovered database: %w", err)
	}
	defer func() { _ = db.Close() }()

	prev := chain[0]
	for _, key := range chain[1:] {
		hdr, payload, err := fetchArtifact(ctx, obj, key)
		if err != nil {
			return err
		}
		if hdr.kind != kindIncremental {
			return fmt.Errorf("backup %s: expected an incremental artifact", key)
		}
		if hdr.parent != prev {
			return fmt.Errorf("backup chain broken: %s chains onto %q, expected %q", key, hdr.parent, prev)
		}

		plain, err := decryptArtifact(key, secret, hdr, payload)
		if err != nil {
			return err
		}
		if err := storage.ApplyIncremental(db, plain); err != nil {
			return fmt.Errorf("failed to apply incremental backup %s: %w", key, err)
		}
		prev = key
	}
	return db.Close()
}

// fetchArtifact downloads an artifact and parses its header.
func fetchArtifact(ctx context.Context, obj *objectstore.Client, key string) (header, []byte, error) {
	rc, err := obj.Get(ctx, key)
	if err != nil {
		return header{}, nil, fmt.Errorf("failed to download backup %s: %w", key, err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return header{}, nil, fmt.Errorf("failed to read backup %s: %w", key, err)
	}
	hdr, payload, err := readHeader(data)
	if err != nil {
		return header{}, nil, fmt.Errorf("invalid backup artifact %s: %w", key, err)
	}
	return hdr, payload, nil
}

// decryptArtifact derives the key from the artifact's salt and decrypts its
// payload.
func decryptArtifact(key, secret string, hdr header, payload []byte) ([]byte, error) {
	crypter, err := storage.NewCrypter([]byte(secret), hdr.salt)
	if err != nil {
		return nil, fmt.Errorf("failed to derive backup decryption key: %w", err)
	}
	plain, err := crypter.Decrypt(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt backup %s (wrong AUTH_SECRET?): %w", key, err)
	}
	return plain, nil
}

// writeFileAtomic writes data to a temp file in the same directory and renames
// it into place, matching the permission bits bbolt.Open expects.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "recover-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
