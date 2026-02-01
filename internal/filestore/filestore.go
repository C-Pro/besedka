package filestore

import (
	"io"
)

// FileStore is an interface for storing and retrieving files by their hash.
type FileStore interface {
	// Save saves the file content with the given hash.
	// It is idempotent: if a file with the same hash already exists, it returns nil.
	Save(r io.Reader, hash string) error

	// Get retrieves the file content for the given hash.
	Get(hash string) (io.ReadCloser, error)
}
