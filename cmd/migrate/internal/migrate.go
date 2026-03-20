package internal

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"

	"besedka/internal/filestore"
	"besedka/internal/storage"

	"go.etcd.io/bbolt"
)

var (
	bucketUsers              = []byte("users")
	bucketMessages           = []byte("messages")
	bucketTokensV2           = []byte("tokens_v2")
	bucketRegistrationTokens = []byte("registration_tokens")
	bucketFiles              = []byte("files")
	bucketSettings           = []byte("settings")
)

func MigrateToEncryption(dbPath, uploadPath string, key []byte) error {
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	var needsMigration bool

	err = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketSettings)
		if b == nil {
			needsMigration = true
			return nil
		}
		salt := b.Get([]byte("salt"))
		if len(salt) == 0 {
			needsMigration = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to read settings bucket: %w", err)
	}

	if !needsMigration {
		return nil // Already encrypted
	}

	fs, err := filestore.NewLocalFileStore(uploadPath)
	if err != nil {
		return fmt.Errorf("failed to open filestore: %w", err)
	}

	crypter, err := storage.NewCrypter(key, nil)
	if err != nil {
		return fmt.Errorf("failed to create crypter: %w", err)
	}

	// Encrypt files
	if err := encryptFileBlobs(db, fs, crypter); err != nil {
		return fmt.Errorf("failed to encrypt files: %w", err)
	}

	// Encrypt buckets
	buckets := [][]byte{
		bucketUsers,
		bucketFiles,
		bucketTokensV2,
		bucketRegistrationTokens,
	}

	for _, bucket := range buckets {
		if err := encryptRecords(db, bucket, crypter); err != nil {
			return fmt.Errorf("failed to encrypt bucket %s: %w", string(bucket), err)
		}
	}

	if err := encryptMessageRecords(db, crypter); err != nil {
		return fmt.Errorf("failed to encrypt messages bucket: %w", err)
	}

	// Persist salt
	newB64salt := base64.StdEncoding.EncodeToString(crypter.Salt())
	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketSettings)
		if b == nil {
			var err error
			b, err = tx.CreateBucket(bucketSettings)
			if err != nil {
				return err
			}
		}
		return b.Put([]byte("salt"), []byte(newB64salt))
	})
	if err != nil {
		return fmt.Errorf("failed to persist salt: %w", err)
	}

	return nil
}

func encryptRecords(db *bbolt.DB, bucket []byte, crypter *storage.Crypter) error {
	return db.Update(func(tx *bbolt.Tx) error {
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

			ct, err := crypter.Encrypt(v)
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

func encryptMessageRecords(db *bbolt.DB, crypter *storage.Crypter) error {
	return db.Update(func(tx *bbolt.Tx) error {
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

				ct, err := crypter.Encrypt(msgVal)
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

func encryptFileBlobs(db *bbolt.DB, fs filestore.FileStore, crypter *storage.Crypter) error {
	hashes := make(map[string]struct{})
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketFiles)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var meta storage.FileMetadata
			if err := meta.UnmarshalBinary(v); err != nil {
				return err
			}
			hashes[meta.Hash] = struct{}{}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("failed to collect file hashes: %w", err)
	}

	for hash := range hashes {
		rc, err := fs.Get(hash)
		if err != nil {
			// In case the physical file is broken, we should either skip or fail.
			// Let's fail fast.
			return fmt.Errorf("failed to get file blob %s: %w", hash, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return fmt.Errorf("failed to read file blob %s: %w", hash, err)
		}

		encrypted, err := crypter.Encrypt(data)
		if err != nil {
			return fmt.Errorf("failed to encrypt file blob %s: %w", hash, err)
		}

		if err := fs.Replace(bytes.NewReader(encrypted), hash); err != nil {
			return fmt.Errorf("failed to replace file blob %s: %w", hash, err)
		}
	}

	return nil
}
