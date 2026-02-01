package filestore

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalFileStore implements FileStore using the local filesystem.
type LocalFileStore struct {
	root string
}

func NewLocalFileStore(root string) (*LocalFileStore, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}
	return &LocalFileStore{root: root}, nil
}

func (s *LocalFileStore) getPath(hash string) string {
	if len(hash) < 2 {
		return filepath.Join(s.root, hash)
	}
	return filepath.Join(s.root, hash[:2], hash)
}

func (s *LocalFileStore) Save(r io.Reader, hash string) error {
	path := s.getPath(hash)

	// Idempotency check
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	// Create parent directory
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write to temporary file first
	tmp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name()) // Clean up if rename fails
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomically rename
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

func (s *LocalFileStore) Get(hash string) (io.ReadCloser, error) {
	path := s.getPath(hash)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", hash, err)
	}
	return f, nil
}
