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
	bucketUsers    = []byte("users")
	bucketChats    = []byte("chats")
	bucketMessages = []byte("messages")
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
		}

		data, err := dbUser.MarshalBinary()
		if err != nil {
			return err
		}
		return b.Put(dbUser.Key(), data)
	})
}

// ListCredentials returns all user credentials stored in the database.
func (s *BboltStorage) ListCredentials() ([]auth.UserCredentials, error) {
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
				},
				PasswordHash: dbUser.PasswordHash,
				TOTPSecret:   dbUser.TOTPSecret,
			})
			return nil
		})
	})
	return credentials, err
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
			// Chat should exist if we are sending messages to it, but maybe strict consistency isn't guaranteed?
			// For now, let's assume chat must exist, or we can't update LastSeq.
			// Or should we implicitly create it? The interface says UpsertChat exists.
			// Let's error if chat not found to be safe.
			return fmt.Errorf("chat %s not found for message upsert", message.ChatID)
		}

		var dbChat DBChat
		if err := dbChat.UnmarshalBinary(chatData); err != nil {
			return fmt.Errorf("failed to unmarshal chat: %w", err)
		}

		// Update LastSeq
		if int(message.Seq) > dbChat.LastSeq {
			dbChat.LastSeq = int(message.Seq)
			// dbChat.LastMessageAt = message.Timestamp // Chat struct doesn't have LastMessageAt, only implicit via LastSeq logic?
			// models.Chat has `LastSeq`.

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
			messages = append(messages, models.Message{
				Seq:       dbMsg.Seq,
				Timestamp: dbMsg.Timestamp,
				ChatID:    dbMsg.ChatID,
				UserID:    dbMsg.UserID,
				Content:   dbMsg.Content,
			})
		}
		return nil
	})
	return messages, err
}
