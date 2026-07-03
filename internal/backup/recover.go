package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"besedka/internal/objectstore"
	"besedka/internal/storage"
)

// RecoverDBIfMissing restores the database file from the newest backup in object
// storage when dbPath does not exist locally. It is a no-op (returns false) when
// object storage is disabled, the database already exists, or no backups are
// present. The secret is the AUTH_SECRET used to derive the decryption key; the
// salt is read from the self-describing backup header.
//
// It must run before the database is opened. A decryption failure is fatal
// (returns an error) rather than silently starting with an empty database.
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
	sort.Strings(keys)
	newest := keys[len(keys)-1] // timestamped keys: newest sorts last

	rc, err := obj.Get(ctx, newest)
	if err != nil {
		return false, fmt.Errorf("failed to download backup %s: %w", newest, err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return false, fmt.Errorf("failed to read backup %s: %w", newest, err)
	}

	hdr, payload, err := readHeader(data)
	if err != nil {
		return false, fmt.Errorf("invalid backup artifact %s: %w", newest, err)
	}

	crypter, err := storage.NewCrypter([]byte(secret), hdr.salt)
	if err != nil {
		return false, fmt.Errorf("failed to derive backup decryption key: %w", err)
	}
	dbBytes, err := crypter.Decrypt(payload)
	if err != nil {
		return false, fmt.Errorf("failed to decrypt backup %s (wrong AUTH_SECRET?): %w", newest, err)
	}

	if err := writeFileAtomic(dbPath, dbBytes, 0600); err != nil {
		return false, fmt.Errorf("failed to write recovered database: %w", err)
	}
	return true, nil
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
