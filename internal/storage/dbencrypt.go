package storage

import (
	"encoding/base64"
	"fmt"

	"go.etcd.io/bbolt"
)

func (s *BboltStorage) encryptStorage() error {
	buckets := [][]byte{
		bucketUsers,
		bucketFiles,
		bucketMessages,
		bucketTokensV2,
		bucketRegistrationTokens,
		// omitting settings bucket, because we need it for decryption (salt)
		// and chat bucket (explained in UpsertChat)
	}
	for _, bucket := range buckets {
		if err := s.encryptRecords(bucket); err != nil {
			return fmt.Errorf("failed to encrypt %s bucket: %w", bucket, err)
		}
	}

	s.isEncrypted = true
	b64salt := base64.StdEncoding.EncodeToString(s.crypter.Salt())

	return s.SetConfig("salt", b64salt)
}

func (s *BboltStorage) encryptRecords(bucket []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.ForEach(func(k, v []byte) error {
			ct, err := s.crypter.Encrypt(v)
			if err != nil {
				return err
			}
			return b.Put(k, ct)
		})
	})
}
