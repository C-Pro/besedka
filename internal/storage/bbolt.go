package storage

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"besedka/internal/auth"
	"besedka/internal/filestore"
	"besedka/internal/models"

	"go.etcd.io/bbolt"
)

var (
	bucketUsers              = []byte("users")
	bucketChats              = []byte("chats")
	bucketMessages           = []byte("messages")
	bucketTokensV2           = []byte("tokens_v2")
	bucketRegistrationTokens = []byte("registration_tokens")
	bucketFiles              = []byte("files")
	bucketSettings           = []byte("settings")
)

type BboltStorage struct {
	db          *bbolt.DB
	crypter     *Crypter
	isEncrypted bool
	fs          filestore.FileStore
}

func NewBboltStorage(path string, key []byte, fs filestore.FileStore) (*BboltStorage, error) {
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt db: %w", err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketUsers); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketChats); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketMessages); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketTokensV2); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketRegistrationTokens); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketFiles); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSettings); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	bs := &BboltStorage{db: db, fs: fs}

	if len(key) > 0 {
		b64salt, err := bs.GetConfig("salt")
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to get salt: %w", err)
		}

		// Empty salt is expected for old versions of the application.
		if b64salt != "" {
			salt, err := base64.StdEncoding.DecodeString(b64salt)
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to decode salt: %w", err)
			}

			crypter, err := NewCrypter(key, salt)
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to create crypter: %w", err)
			}
			bs.crypter = crypter
			bs.isEncrypted = true
		} else {
			// If salt is not set, we need to check if DB is empty
			isEmpty := true
			err := db.View(func(tx *bbolt.Tx) error {
				b := tx.Bucket(bucketUsers)
				c := b.Cursor()
				k, _ := c.First()
				if k != nil {
					isEmpty = false
				}
				return nil
			})
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to check if storage is empty: %w", err)
			}

			if !isEmpty {
				_ = db.Close()
				return nil, errors.New("data encryption salt is not set and database is not empty. Please run the migration tool")
			}

			// Empty database: generate random salt and persist it
			salt, err := genSalt()
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to generate salt: %w", err)
			}
			b64salt = base64.StdEncoding.EncodeToString(salt)

			if err := bs.SetConfig("salt", b64salt); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to persist new salt: %w", err)
			}

			crypter, err := NewCrypter(key, salt)
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to recreate crypter with new salt: %w", err)
			}
			bs.crypter = crypter
			bs.isEncrypted = true
		}
	}

	return bs, nil
}

func (s *BboltStorage) Close() error {
	return s.db.Close()
}

// GetConfig returns the config value by key.
func (s *BboltStorage) GetConfig(key string) (string, error) {
	var value string
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketSettings)
		value = string(b.Get([]byte(key)))
		return nil
	})
	return value, err
}

// SetConfig sets the value in the config by key.
func (s *BboltStorage) SetConfig(key string, value string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketSettings)
		return b.Put([]byte(key), []byte(value))
	})
}

// UpsertCredentials stores new or updated user credentials.
func (s *BboltStorage) UpsertCredentials(credentials auth.UserCredentials) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketUsers)
		dbUser := &DBUser{
			ID:           credentials.ID,
			UserName:     credentials.UserName,
			DisplayName:  credentials.DisplayName,
			AvatarURL:    credentials.AvatarURL,
			LastSeen:     credentials.Presence.LastSeen,
			PasswordHash: credentials.PasswordHash,
			TOTPSecret:   credentials.TOTPSecret,
			LastTOTP:     credentials.LastTOTP,
			Status:       string(credentials.Status),
		}

		data, err := dbUser.MarshalBinary()
		if err != nil {
			return err
		}

		if s.isEncrypted {
			var err error
			data, err = s.crypter.Encrypt(data)
			if err != nil {
				return fmt.Errorf("failed to encrypt user record: %w", err)
			}
		}

		return b.Put(dbUser.Key(), data)
	})
}

// backfillStatus returns the appropriate status for a user based on their current data.
// For pre-existing DB records created before the Status field existed, msgpack will leave
// this as an empty string. We derive the status from LastTOTP: -1 => created, otherwise active.
func backfillStatus(dbUser *DBUser) models.UserStatus {
	if dbUser.Status != "" {
		return models.UserStatus(dbUser.Status)
	}
	// Pre-existing record without status - derive from LastTOTP
	if dbUser.LastTOTP == -1 {
		return models.UserStatusCreated
	}
	return models.UserStatusActive
}

// ListAllCredentials returns all user credentials stored in the database.
func (s *BboltStorage) ListAllCredentials() ([]auth.UserCredentials, error) {
	var credentials []auth.UserCredentials
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketUsers)
		return b.ForEach(func(k, v []byte) error {
			var dbUser DBUser

			if s.isEncrypted {
				var err error
				v, err = s.crypter.Decrypt(v)
				if err != nil {
					return fmt.Errorf("failed to decrypt user record: %w", err)
				}
			}

			if err := dbUser.UnmarshalBinary(v); err != nil {
				return err
			}
			credentials = append(credentials, auth.UserCredentials{
				User: models.User{
					ID:          dbUser.ID,
					UserName:    dbUser.UserName,
					DisplayName: dbUser.DisplayName,
					AvatarURL:   dbUser.AvatarURL,
					Presence: models.Presence{
						LastSeen: dbUser.LastSeen,
					},
					Status: backfillStatus(&dbUser),
				},
				PasswordHash: dbUser.PasswordHash,
				TOTPSecret:   dbUser.TOTPSecret,
				LastTOTP:     dbUser.LastTOTP,
			})
			return nil
		})
	})
	return credentials, err
}

// ListCredentials returns only active user credentials stored in the database.
func (s *BboltStorage) ListCredentials() ([]auth.UserCredentials, error) {
	all, err := s.ListAllCredentials()
	if err != nil {
		return nil, err
	}
	var active []auth.UserCredentials
	for _, c := range all {
		if c.Status == models.UserStatusActive {
			active = append(active, c)
		}
	}
	return active, nil
}

// UpsertChat saves chat struct to the database.
func (s *BboltStorage) UpsertChat(chat models.Chat) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketChats)
		dbChat := DBChat{
			ID:        chat.ID,
			Name:      chat.Name,
			AvatarURL: chat.AvatarURL,
			LastSeq:   chat.LastSeq,
			IsDM:      chat.IsDM,
		}
		data, err := dbChat.MarshalBinary()
		if err != nil {
			return err
		}

		// We do not encrypt chat metadata. It only contains IDs and last sequence number,
		// and sequence number can be inferred by number of messages anyway.
		// Encrypting chat metadata would require decrypting/encrypting it for every message upsert,
		// which is too much fuss.
		return b.Put(dbChat.Key(), data)
	})
}

// ListChats returns all chats stored in the database.
func (s *BboltStorage) ListChats() ([]models.Chat, error) {
	var chats []models.Chat
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketChats)
		return b.ForEach(func(k, v []byte) error {
			var dbChat DBChat
			if err := dbChat.UnmarshalBinary(v); err != nil {
				return err
			}
			chats = append(chats, models.Chat{
				ID:        dbChat.ID,
				Name:      dbChat.Name,
				AvatarURL: dbChat.AvatarURL,
				LastSeq:   dbChat.LastSeq,
				IsDM:      dbChat.IsDM,
			})
			return nil
		})
	})
	return chats, err
}

// UpsertMessage saves chat message to the database and updates chat object last message sequence number and timestamp.
func (s *BboltStorage) UpsertMessage(message models.Message) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		if message.ChatID == "" {
			return errors.New("message missing chatID")
		}

		// 1. Save message
		mainMsgBucket := tx.Bucket(bucketMessages)
		chatBucket, err := mainMsgBucket.CreateBucketIfNotExists([]byte(message.ChatID))
		if err != nil {
			return fmt.Errorf("failed to create chat bucket: %w", err)
		}

		dbMessage := DBMessage{
			Seq:       message.Seq,
			Timestamp: message.Timestamp,
			ChatID:    message.ChatID,
			UserID:    message.UserID,
			Content:   message.Content,
		}

		if len(message.Attachments) > 0 {
			dbMessage.Attachments = make([]DBAttachment, len(message.Attachments))
			for i, a := range message.Attachments {
				dbMessage.Attachments[i] = DBAttachment{
					Type:     string(a.Type),
					Name:     a.Name,
					MimeType: a.MimeType,
					FileID:   a.FileID,
				}
			}
		}

		data, err := dbMessage.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		if s.isEncrypted {
			var err error
			data, err = s.crypter.Encrypt(data)
			if err != nil {
				return fmt.Errorf("failed to encrypt message record: %w", err)
			}
		}

		if err := chatBucket.Put(dbMessage.Key(), data); err != nil {
			return fmt.Errorf("failed to put message: %w", err)
		}

		// 2. Update chat LastSeq
		chatBucketStats := tx.Bucket(bucketChats)
		chatKey := []byte(message.ChatID)
		chatData := chatBucketStats.Get(chatKey)
		if chatData == nil {
			return fmt.Errorf("chat %s not found for message upsert", message.ChatID)
		}

		var dbChat DBChat
		if err := dbChat.UnmarshalBinary(chatData); err != nil {
			return fmt.Errorf("failed to unmarshal chat: %w", err)
		}

		// Update LastSeq
		if int(message.Seq) > dbChat.LastSeq {
			dbChat.LastSeq = int(message.Seq)

			newData, err := dbChat.MarshalBinary()
			if err != nil {
				return err
			}
			if err := chatBucketStats.Put(chatKey, newData); err != nil {
				return err
			}
		}

		return nil
	})
}

// ListMessages returns chat messages stored in the database.
func (s *BboltStorage) ListMessages(chatID string, from, to int64) ([]models.Message, error) {
	var messages []models.Message
	err := s.db.View(func(tx *bbolt.Tx) error {
		mainMsgBucket := tx.Bucket(bucketMessages)
		chatBucket := mainMsgBucket.Bucket([]byte(chatID))
		if chatBucket == nil {
			return nil // No messages for this chat
		}

		c := chatBucket.Cursor()

		minKey := make([]byte, 8)
		binary.BigEndian.PutUint64(minKey, uint64(from))

		maxKey := make([]byte, 8)
		binary.BigEndian.PutUint64(maxKey, uint64(to))

		for k, v := c.Seek(minKey); k != nil && bytes.Compare(k, maxKey) <= 0; k, v = c.Next() {
			if s.isEncrypted {
				var err error
				v, err = s.crypter.Decrypt(v)
				if err != nil {
					return fmt.Errorf("failed to decrypt message record: %w", err)
				}
			}

			var dbMsg DBMessage
			if err := dbMsg.UnmarshalBinary(v); err != nil {
				return err
			}
			msg := models.Message{
				Seq:       dbMsg.Seq,
				Timestamp: dbMsg.Timestamp,
				ChatID:    dbMsg.ChatID,
				UserID:    dbMsg.UserID,
				Content:   dbMsg.Content,
			}
			if len(dbMsg.Attachments) > 0 {
				msg.Attachments = make([]models.Attachment, len(dbMsg.Attachments))
				for i, a := range dbMsg.Attachments {
					msg.Attachments[i] = models.Attachment{
						Type:     models.AttachmentType(a.Type),
						Name:     a.Name,
						MimeType: a.MimeType,
						FileID:   a.FileID,
					}
				}
			}
			messages = append(messages, msg)
		}
		return nil
	})
	return messages, err
}

func (s *BboltStorage) UpsertToken(userID string, tokenHash string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTokensV2)
		dbToken := &DBToken{
			UserID: userID,
			Token:  tokenHash,
		}
		data, err := dbToken.MarshalBinary()
		if err != nil {
			return err
		}
		if s.isEncrypted {
			var err error
			data, err = s.crypter.Encrypt(data)
			if err != nil {
				return fmt.Errorf("failed to encrypt token record: %w", err)
			}
		}
		// Key is now tokenHash
		return b.Put(dbToken.Key(), data)
	})
}

// DeleteToken now takes a tokenHash and deletes that specific token.
func (s *BboltStorage) DeleteToken(tokenHash string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTokensV2)
		return b.Delete([]byte(tokenHash))
	})
}

func (s *BboltStorage) ListTokens() (map[string]string, error) {
	tokens := make(map[string]string)
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTokensV2)
		return b.ForEach(func(k, v []byte) error {
			if s.isEncrypted {
				var err error
				v, err = s.crypter.Decrypt(v)
				if err != nil {
					return fmt.Errorf("failed to decrypt token record: %w", err)
				}
			}

			var dbToken DBToken
			if err := dbToken.UnmarshalBinary(v); err != nil {
				return err
			}
			// key (k) is also token hash, but let's use the one from struct
			tokens[dbToken.Token] = dbToken.UserID
			return nil
		})
	})
	return tokens, err
}

func (s *BboltStorage) UpsertRegistrationToken(userID string, token string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRegistrationTokens)
		dbToken := &DBToken{
			UserID: userID,
			Token:  token,
		}
		data, err := dbToken.MarshalBinary()
		if err != nil {
			return err
		}
		if s.isEncrypted {
			var err error
			data, err = s.crypter.Encrypt(data)
			if err != nil {
				return fmt.Errorf("failed to encrypt token record: %w", err)
			}
		}

		// Use UserID as key
		return b.Put([]byte(userID), data)
	})
}

func (s *BboltStorage) DeleteRegistrationToken(userID string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRegistrationTokens)
		return b.Delete([]byte(userID))
	})
}

func (s *BboltStorage) ListRegistrationTokens() (map[string]string, error) {
	tokens := make(map[string]string)
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRegistrationTokens)
		return b.ForEach(func(k, v []byte) error {
			if s.isEncrypted {
				var err error
				v, err = s.crypter.Decrypt(v)
				if err != nil {
					return fmt.Errorf("failed to decrypt token record: %w", err)
				}
			}

			var dbToken DBToken
			if err := dbToken.UnmarshalBinary(v); err != nil {
				return err
			}
			tokens[dbToken.UserID] = dbToken.Token
			return nil
		})
	})
	return tokens, err
}
