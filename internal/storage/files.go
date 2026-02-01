package storage

import (
	"fmt"

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
		return meta.UnmarshalBinary(data)
	})
	return meta, err
}
