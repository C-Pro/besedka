package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"besedka/internal/auth"
	"besedka/internal/filestore"
	"besedka/internal/models"

	"go.etcd.io/bbolt"
)

var testSecret = []byte(`secret`)

func TestStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := filepath.Join(tmpDir, "test.db")
	fs, _ := filestore.NewLocalFileStore(filepath.Join(tmpDir, "fs"))
	store, err := NewBboltStorage(dbPath, testSecret, fs)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("Credentials", func(t *testing.T) {
		creds := auth.UserCredentials{
			User: models.User{
				ID:          "user1",
				UserName:    "alice",
				DisplayName: "Alice",
				Status:      models.UserStatusActive,
			},
			PasswordHash: "hash",
			TOTPSecret:   "secret",
		}

		if err := store.UpsertCredentials(creds); err != nil {
			t.Fatalf("UpsertCredentials failed: %v", err)
		}

		listCreds, err := store.ListCredentials()
		if err != nil {
			t.Fatalf("ListCredentials failed: %v", err)
		}
		if len(listCreds) != 1 {
			t.Errorf("expected 1 credential, got %d", len(listCreds))
		}
		if listCreds[0].Status != models.UserStatusActive {
			t.Errorf("expected Status %s, got %s", models.UserStatusActive, listCreds[0].Status)
		}
		if listCreds[0].ID != creds.ID {
			t.Errorf("expected ID %s, got %s", creds.ID, listCreds[0].ID)
		}
		if listCreds[0].TOTPSecret != creds.TOTPSecret {
			t.Errorf("expected TOTPSecret %s, got %s", creds.TOTPSecret, listCreds[0].TOTPSecret)
		}

		// Test filtering
		inactiveCreds := auth.UserCredentials{
			User: models.User{
				ID:          "user2",
				UserName:    "bob",
				DisplayName: "Bob",
				Status:      models.UserStatusCreated,
			},
			PasswordHash: "hash",
			TOTPSecret:   "secret",
		}
		if err := store.UpsertCredentials(inactiveCreds); err != nil {
			t.Fatalf("UpsertCredentials inactive failed: %v", err)
		}

		// ListCredentials should still return 1 (Active only)
		listCreds, err = store.ListCredentials()
		if err != nil {
			t.Fatalf("ListCredentials failed: %v", err)
		}
		if len(listCreds) != 1 {
			t.Errorf("expected 1 active credential, got %d", len(listCreds))
		}

		// ListAllCredentials should return 2
		listAll, err := store.ListAllCredentials()
		if err != nil {
			t.Fatalf("ListAllCredentials failed: %v", err)
		}
		if len(listAll) != 2 {
			t.Errorf("expected 2 credentials, got %d", len(listAll))
		}
	})

	t.Run("Chat", func(t *testing.T) {
		chat := models.Chat{
			ID:   "chat1",
			Name: "General",
		}
		if err := store.UpsertChat(chat); err != nil {
			t.Fatalf("UpsertChat failed: %v", err)
		}

		listChats, err := store.ListChats()
		if err != nil {
			t.Fatalf("ListChats failed: %v", err)
		}
		if len(listChats) != 1 {
			t.Errorf("expected 1 chat, got %d", len(listChats))
		}
	})

	t.Run("Messages", func(t *testing.T) {
		msg1 := models.Message{
			Seq:       1,
			Timestamp: time.Now().Unix(),
			ChatID:    "chat1",
			UserID:    "user1",
			Content:   "hello",
		}
		if err := store.UpsertMessage(msg1); err != nil {
			t.Fatalf("UpsertMessage 1 failed: %v", err)
		}

		msg2 := models.Message{
			Seq:       2,
			Timestamp: time.Now().Unix(),
			ChatID:    "chat1",
			UserID:    "user1",
			Content:   "world",
		}
		if err := store.UpsertMessage(msg2); err != nil {
			t.Fatalf("UpsertMessage 2 failed: %v", err)
		}

		msgs, err := store.ListMessages("chat1", 0, 100)
		if err != nil {
			t.Fatalf("ListMessages failed: %v", err)
		}
		if len(msgs) != 2 {
			t.Errorf("expected 2 messages, got %d", len(msgs))
		}
		if msgs[0].Content != "hello" {
			t.Errorf("expected msg1 content 'hello', got %s", msgs[0].Content)
		}

		// Check range
		msgsRange, err := store.ListMessages("chat1", 2, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgsRange) != 1 {
			t.Errorf("expected 1 message in range [2, 10), got %d", len(msgsRange))
		}
		if msgsRange[0].Seq != 2 {
			t.Errorf("expected msg seq 2, got %d", msgsRange[0].Seq)
		}

		// Check chat update (LastSeq)
		listChats3, _ := store.ListChats()
		if listChats3[0].LastSeq != 2 {
			t.Errorf("expected chat LastSeq 2, got %d", listChats3[0].LastSeq)
		}
	})

	t.Run("Tokens", func(t *testing.T) {
		userID := "user2" // using user2 to avoid confusion with previous subtest though store is same
		tokenHash := "token_hash_123"

		if err := store.UpsertToken(userID, tokenHash); err != nil {
			t.Fatalf("UpsertToken failed: %v", err)
		}

		tokens, err := store.ListTokens()
		if err != nil {
			t.Fatalf("ListTokens failed: %v", err)
		}
		if tokens[tokenHash] != userID {
			t.Errorf("expected userID %s for token %s, got %s", userID, tokenHash, tokens[tokenHash])
		}

		if err := store.DeleteToken(tokenHash); err != nil {
			t.Fatalf("DeleteToken failed: %v", err)
		}

		tokens, err = store.ListTokens()
		if err != nil {
			t.Fatalf("ListTokens failed: %v", err)
		}
		if _, ok := tokens[tokenHash]; ok {
			t.Errorf("expected token to be deleted")
		}
	})

	t.Run("Attachments", func(t *testing.T) {
		msg := models.Message{
			Seq:       3,
			Timestamp: time.Now().Unix(),
			ChatID:    "chat1",
			UserID:    "user1",
			Content:   "check out this image",
			Attachments: []models.Attachment{
				{
					Type:     models.AttachmentTypeImage,
					Name:     "test.png",
					MimeType: "image/png",
					FileID:   "uuid-123",
				},
			},
		}

		if err := store.UpsertMessage(msg); err != nil {
			t.Fatalf("UpsertMessage failed: %v", err)
		}

		msgs, err := store.ListMessages("chat1", 3, 3)
		if err != nil {
			t.Fatalf("ListMessages failed: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if len(msgs[0].Attachments) != 1 {
			t.Fatalf("expected 1 attachment, got %d", len(msgs[0].Attachments))
		}
		att := msgs[0].Attachments[0]
		if att.Name != "test.png" {
			t.Errorf("expected attachment name test.png, got %s", att.Name)
		}
		if att.FileID != "uuid-123" {
			t.Errorf("expected attachment fileID uuid-123, got %s", att.FileID)
		}
	})

	t.Run("StatusBackfill", func(t *testing.T) {
		// Test that old DB records without Status field get backfilled correctly
		// Simulate old records by directly inserting DBUser with empty Status
		err := store.db.Update(func(tx *bbolt.Tx) error {
			b := tx.Bucket(bucketUsers)

			// Old record with LastTOTP = -1 (created state)
			oldCreatedUser := &DBUser{
				ID:           "old_created",
				UserName:     "old_created_user",
				DisplayName:  "Old Created",
				PasswordHash: "hash",
				TOTPSecret:   "secret",
				LastTOTP:     -1,
				Status:       "", // Empty status to simulate old record
			}
			data, err := oldCreatedUser.MarshalBinary()
			if err != nil {
				return err
			}
			if store.isEncrypted {
				data, err = store.crypter.Encrypt(data)
				if err != nil {
					return err
				}
			}
			if err := b.Put(oldCreatedUser.Key(), data); err != nil {
				return err
			}

			// Old record with LastTOTP = 0 (active state)
			oldActiveUser := &DBUser{
				ID:           "old_active",
				UserName:     "old_active_user",
				DisplayName:  "Old Active",
				PasswordHash: "hash",
				TOTPSecret:   "secret",
				LastTOTP:     0,
				Status:       "", // Empty status to simulate old record
			}
			data, err = oldActiveUser.MarshalBinary()
			if err != nil {
				return err
			}
			if store.isEncrypted {
				data, err = store.crypter.Encrypt(data)
				if err != nil {
					return err
				}
			}
			return b.Put(oldActiveUser.Key(), data)
		})
		if err != nil {
			t.Fatalf("failed to insert old records: %v", err)
		}

		// Verify backfilled status
		allCreds, err := store.ListAllCredentials()
		if err != nil {
			t.Fatalf("ListAllCredentials failed: %v", err)
		}

		// Find the backfilled users
		var oldCreated, oldActive *auth.UserCredentials
		for i := range allCreds {
			if allCreds[i].ID == "old_created" {
				oldCreated = &allCreds[i]
			}
			if allCreds[i].ID == "old_active" {
				oldActive = &allCreds[i]
			}
		}

		if oldCreated != nil {
			if oldCreated.Status != models.UserStatusCreated {
				t.Errorf("expected old_created status to be %s, got %s", models.UserStatusCreated, oldCreated.Status)
			}
		} else {
			t.Fatal("old_created user not found")
		}

		if oldActive != nil {
			if oldActive.Status != models.UserStatusActive {
				t.Errorf("expected old_active status to be %s, got %s", models.UserStatusActive, oldActive.Status)
			}
		} else {
			t.Fatal("old_active user not found")
		}
	})

	t.Run("VAPID", func(t *testing.T) {
		priv := "private_key_123"
		pub := "public_key_456"
		if err := store.SaveVAPIDKeys(priv, pub); err != nil {
			t.Fatalf("SaveVAPIDKeys failed: %v", err)
		}

		gotPriv, gotPub, err := store.GetVAPIDKeys()
		if err != nil {
			t.Fatalf("GetVAPIDKeys failed: %v", err)
		}
		if gotPriv != priv || gotPub != pub {
			t.Errorf("expected %s/%s, got %s/%s", priv, pub, gotPriv, gotPub)
		}
	})

	t.Run("PushSubscriptions", func(t *testing.T) {
		userID := "user1"
		endpoint := "https://example.com/push/123"
		subData := []byte(`{"endpoint":"https://example.com/push/123","keys":{"p256dh":"...","auth":"..."}}`)

		if err := store.UpsertPushSubscription(userID, endpoint, subData); err != nil {
			t.Fatalf("UpsertPushSubscription failed: %v", err)
		}

		subs, err := store.GetPushSubscriptions(userID)
		if err != nil {
			t.Fatalf("GetPushSubscriptions failed: %v", err)
		}
		if len(subs) != 1 {
			t.Fatalf("expected 1 subscription, got %d", len(subs))
		}
		if string(subs[0]) != string(subData) {
			t.Errorf("expected subscription data %s, got %s", string(subData), string(subs[0]))
		}

		if err := store.DeletePushSubscription(userID, endpoint); err != nil {
			t.Fatalf("DeletePushSubscription failed: %v", err)
		}

		subs, err = store.GetPushSubscriptions(userID)
		if err != nil {
			t.Fatalf("GetPushSubscriptions failed: %v", err)
		}
		if len(subs) != 0 {
			t.Errorf("expected 0 subscriptions after delete, got %d", len(subs))
		}
	})
}

func TestNewBboltStorage_UnencryptedDbFailsWithKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_fail_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := filepath.Join(tmpDir, "fail_test.db")
	fs, _ := filestore.NewLocalFileStore(filepath.Join(tmpDir, "fs"))

	// 1. Create unencrypted populated DB
	store, err := NewBboltStorage(dbPath, nil, fs)
	if err != nil {
		t.Fatal(err)
	}
	creds := auth.UserCredentials{
		User:         models.User{ID: "fail_user"},
		PasswordHash: "empty",
	}
	if err := store.UpsertCredentials(creds); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	// 2. Open populated DB with key - should FAIL
	key := []byte("12345678901234567890123456789012") // 32 bytes
	_, err = NewBboltStorage(dbPath, key, fs)
	if err == nil {
		t.Fatal("expected NewBboltStorage to fail when opening unencrypted database with key")
	}
	if err.Error() != "data encryption salt is not set and database is not empty. Please run the migration tool" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

