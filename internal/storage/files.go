package storage

import (
	"bytes"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
	"go.etcd.io/bbolt"
)

type FileMetadata struct {
	ID        string `msgpack:"id"`
	Hash      string `msgpack:"hash"`
	MimeType  string `msgpack:"mimeType"`
	Size      int64  `msgpack:"size"`
	CreatedAt int64  `msgpack:"createdAt"`
	UserID    string `msgpack:"userId"`
	ChatID    string `msgpack:"chatId"`
}

func (f *FileMetadata) Key() []byte {
	return []byte(f.ID)
}

func (f *FileMetadata) MarshalBinary() (data []byte, err error) {
	type alias FileMetadata
	return msgpack.Marshal((*alias)(f))
}

func (f *FileMetadata) UnmarshalBinary(data []byte) error {
	type alias FileMetadata
	return msgpack.Unmarshal(data, (*alias)(f))
}

func (s *BboltStorage) UpsertFileMetadata(meta FileMetadata) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketFiles)
		data, err := meta.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to marshal file metadata: %w", err)
		}
		if s.isEncrypted {
			var err error
			data, err = s.crypter.Encrypt(data)
			if err != nil {
				return fmt.Errorf("failed to encrypt file metadata: %w", err)
			}
		}
		return b.Put(meta.Key(), data)
	})
}

func (s *BboltStorage) GetFileMetadata(id string) (FileMetadata, error) {
	var meta FileMetadata
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketFiles)
		data := b.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("file metadata not found for id %s", id)
		}
		if s.isEncrypted {
			var err error
			data, err = s.crypter.Decrypt(data)
			if err != nil {
				return fmt.Errorf("failed to decrypt file metadata: %w", err)
			}
		}
		return meta.UnmarshalBinary(data)
	})
	return meta, err
}

// SaveFileBlob saves a file blob, encrypting it if storage is encrypted.
func (s *BboltStorage) SaveFileBlob(r io.Reader, hash string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("failed to read file blob: %w", err)
	}

	if s.isEncrypted {
		data, err = s.crypter.Encrypt(data)
		if err != nil {
			return fmt.Errorf("failed to encrypt file blob: %w", err)
		}
	}

	return s.fs.Save(bytes.NewReader(data), hash)
}

// GetFileBlob gets a file blob, decrypting it if storage is encrypted.
func (s *BboltStorage) GetFileBlob(hash string) (io.ReadCloser, error) {
	rc, err := s.fs.Get(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get file blob from filestore: %w", err)
	}

	if !s.isEncrypted {
		return rc, nil
	}

	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read encrypted file blob: %w", err)
	}

	data, err = s.crypter.Decrypt(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt file blob: %w", err)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}
