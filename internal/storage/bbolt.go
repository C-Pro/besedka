package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"besedka/internal/auth"
	"besedka/internal/models"

	"go.etcd.io/bbolt"
)

var (
	bucketUsers              = []byte("users")
	bucketChats              = []byte("chats")
	bucketMessages           = []byte("messages")
	bucketTokens             = []byte("tokens")
	bucketTokensV2           = []byte("tokens_v2")
	bucketRegistrationTokens = []byte("registration_tokens")
	bucketFiles              = []byte("files")
)

type BboltStorage struct {
	db *bbolt.DB
}

func NewBboltStorage(path string) (*BboltStorage, error) {
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
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	return &BboltStorage{db: db}, nil
}

func (s *BboltStorage) Close() error {
	return s.db.Close()
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
		return b.Put(dbUser.Key(), data)
	})
}

// ListAllCredentials returns all user credentials stored in the database.
func (s *BboltStorage) ListAllCredentials() ([]auth.UserCredentials, error) {
	var credentials []auth.UserCredentials
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketUsers)
		return b.ForEach(func(k, v []byte) error {
			var dbUser DBUser
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
					Status: models.UserStatus(dbUser.Status),
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
			ID:      chat.ID,
			Name:    chat.Name,
			LastSeq: chat.LastSeq,
			IsDM:    chat.IsDM,
		}
		data, err := dbChat.MarshalBinary()
		if err != nil {
			return err
		}
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
				ID:      dbChat.ID,
				Name:    dbChat.Name,
				LastSeq: dbChat.LastSeq,
				IsDM:    dbChat.IsDM,
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

func (s *BboltStorage) MigrateTokens(hasher func(token string) string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		oldBucket := tx.Bucket(bucketTokens)
		if oldBucket == nil {
			return nil // nothing to migrate
		}

		newBucket := tx.Bucket(bucketTokensV2)

		// Iterate over old tokens
		err := oldBucket.ForEach(func(k, v []byte) error {
			// In old schema: Key=UserID, Value=DBToken{UserID, Token}
			var oldToken DBToken
			if err := oldToken.UnmarshalBinary(v); err != nil {
				// If unmarshal fails, we can't migrate this token. Log or skip?
				// Returning error aborts migration.
				return fmt.Errorf("corrupt token for user %s: %w", string(k), err)
			}

			// oldToken.Token is the Raw Token
			hashedToken := hasher(oldToken.Token)

			newToken := DBToken{
				UserID: oldToken.UserID,
				Token:  hashedToken,
			}

			data, err := newToken.MarshalBinary()
			if err != nil {
				return err
			}

			if err := newBucket.Put(newToken.Key(), data); err != nil {
				return err
			}

			return nil
		})

		if err != nil {
			return err
		}

		// Delete old bucket after successful migration
		return tx.DeleteBucket(bucketTokens)
	})
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
