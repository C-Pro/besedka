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

	if err := s.encryptMessageRecords(); err != nil {
		return fmt.Errorf("failed to encrypt messages bucket: %w", err)
	}

	s.isEncrypted = true
	b64salt := base64.StdEncoding.EncodeToString(s.crypter.Salt())

	return s.SetConfig("salt", b64salt)
}

func (s *BboltStorage) encryptRecords(bucket []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}

		type kv struct {
			k, v []byte
		}
		var items []kv

		err := b.ForEach(func(k, v []byte) error {
			if v == nil {
				return nil // skip sub-buckets
			}
			kCopy := make([]byte, len(k))
			copy(kCopy, k)

			ct, err := s.crypter.Encrypt(v)
			if err != nil {
				return err
			}
			items = append(items, kv{k: kCopy, v: ct})
			return nil
		})
		if err != nil {
			return err
		}

		for _, item := range items {
			if err := b.Put(item.k, item.v); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BboltStorage) encryptMessageRecords() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		mainMsgBucket := tx.Bucket(bucketMessages)
		if mainMsgBucket == nil {
			return nil
		}

		return mainMsgBucket.ForEach(func(k, v []byte) error {
			if v != nil {
				return nil // not a bucket
			}
			chatBucket := mainMsgBucket.Bucket(k)
			if chatBucket == nil {
				return nil
			}

			type kv struct {
				k, v []byte
			}
			var items []kv

			err := chatBucket.ForEach(func(msgKey, msgVal []byte) error {
				if msgVal == nil {
					return nil // nested sub-bucket somehow? skip
				}
				kCopy := make([]byte, len(msgKey))
				copy(kCopy, msgKey)

				ct, err := s.crypter.Encrypt(msgVal)
				if err != nil {
					return err
				}
				items = append(items, kv{k: kCopy, v: ct})
				return nil
			})
			if err != nil {
				return err
			}

			for _, item := range items {
				if err := chatBucket.Put(item.k, item.v); err != nil {
					return err
				}
			}
			return nil
		})
	})
}
