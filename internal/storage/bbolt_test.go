package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"besedka/internal/auth"
	"besedka/internal/filestore"
	"besedka/internal/models"

	"github.com/vmihailenco/msgpack/v5"
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
			data, err = store.crypter.Encrypt(data)
			if err != nil {
				return err
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
			data, err = store.crypter.Encrypt(data)
			if err != nil {
				return err
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

	t.Run("LastSeen", func(t *testing.T) {
		batch := []models.LastSeenEntry{
			{UserID: "user1", ChatID: "chat1", Seq: 10},
			{UserID: "user2", ChatID: "chat1", Seq: 20},
		}

		if err := store.SaveLastSeenBatch(batch); err != nil {
			t.Fatalf("SaveLastSeenBatch failed: %v", err)
		}

		list, err := store.ListLastSeen()
		if err != nil {
			t.Fatalf("ListLastSeen failed: %v", err)
		}

		if len(list) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(list))
		}

		var u1Entry, u2Entry *models.LastSeenEntry
		for i := range list {
			if list[i].UserID == "user1" {
				u1Entry = &list[i]
			}
			if list[i].UserID == "user2" {
				u2Entry = &list[i]
			}
		}

		if u1Entry == nil || u1Entry.ChatID != "chat1" || u1Entry.Seq != 10 {
			t.Errorf("incorrect entry for user1: %+v", u1Entry)
		}
		if u2Entry == nil || u2Entry.ChatID != "chat1" || u2Entry.Seq != 20 {
			t.Errorf("incorrect entry for user2: %+v", u2Entry)
		}

		updateBatch := []models.LastSeenEntry{
			{UserID: "user1", ChatID: "chat1", Seq: 15},
		}
		if err := store.SaveLastSeenBatch(updateBatch); err != nil {
			t.Fatalf("SaveLastSeenBatch update failed: %v", err)
		}

		list, err = store.ListLastSeen()
		if err != nil {
			t.Fatalf("ListLastSeen after update failed: %v", err)
		}

		for _, entry := range list {
			if entry.UserID == "user1" && entry.ChatID == "chat1" {
				if entry.Seq != 15 {
					t.Errorf("expected updated seq 15, got %d", entry.Seq)
				}
			}
		}
	})

	t.Run("FileMetadata", func(t *testing.T) {
		withThumb := FileMetadata{
			ID:            "file1",
			Hash:          "hash1",
			MimeType:      "image/png",
			Size:          200_000,
			CreatedAt:     time.Now().Unix(),
			UserID:        "user1",
			ThumbnailHash: "thumbhash1",
			ThumbnailMime: "image/jpeg",
			ThumbnailSize: 80_000,
		}
		withoutThumb := FileMetadata{
			ID:       "file2",
			Hash:     "hash2",
			MimeType: "application/pdf",
			Size:     1000,
			UserID:   "user1",
		}

		for _, meta := range []FileMetadata{withThumb, withoutThumb} {
			if err := store.UpsertFileMetadata(meta); err != nil {
				t.Fatalf("UpsertFileMetadata failed: %v", err)
			}
		}

		got, err := store.GetFileMetadata("file1")
		if err != nil {
			t.Fatalf("GetFileMetadata failed: %v", err)
		}
		if got != withThumb {
			t.Errorf("metadata roundtrip mismatch: got %+v, want %+v", got, withThumb)
		}

		got, err = store.GetFileMetadata("file2")
		if err != nil {
			t.Fatalf("GetFileMetadata failed: %v", err)
		}
		if got.ThumbnailHash != "" || got.ThumbnailMime != "" || got.ThumbnailSize != 0 {
			t.Errorf("expected empty thumbnail fields, got %+v", got)
		}

		metas, err := store.ListFileMetadata()
		if err != nil {
			t.Fatalf("ListFileMetadata failed: %v", err)
		}
		if len(metas) != 2 {
			t.Errorf("expected 2 file metadata records, got %d", len(metas))
		}
	})

	t.Run("UserSettings", func(t *testing.T) {
		// No record yet -> found == false so callers can apply defaults.
		if _, found, err := store.GetUserSettings("settings_user"); err != nil {
			t.Fatalf("GetUserSettings failed: %v", err)
		} else if found {
			t.Error("expected no settings record for a new user")
		}

		want := models.DefaultUserSettings()
		want.Notifications.SoundAllMessages = true
		want.Notifications.SuppressWhenChatOpen = false
		want.Appearance.Theme = models.ThemeLight
		if err := store.UpsertUserSettings("settings_user", want); err != nil {
			t.Fatalf("UpsertUserSettings failed: %v", err)
		}

		got, found, err := store.GetUserSettings("settings_user")
		if err != nil {
			t.Fatalf("GetUserSettings failed: %v", err)
		}
		if !found {
			t.Fatal("expected settings record to exist after upsert")
		}
		if got != want {
			t.Errorf("settings roundtrip mismatch: got %+v, want %+v", got, want)
		}

		// Records written before appearance settings existed normalize to dark,
		// preserving the historical UI instead of following the device theme.
		legacy := (&DBUserSettings{
			UserID: "legacy_settings_user",
			Notifications: DBNotificationSettings{
				SoundDirectMessages:  true,
				SoundMentions:        true,
				SuppressWhenChatOpen: true,
			},
		}).toModel()
		if legacy.Appearance.Theme != models.ThemeDark {
			t.Errorf("legacy theme = %q, want %q", legacy.Appearance.Theme, models.ThemeDark)
		}

		// Update overwrites the stored value.
		want.Notifications.SoundMentions = false
		if err := store.UpsertUserSettings("settings_user", want); err != nil {
			t.Fatalf("UpsertUserSettings update failed: %v", err)
		}
		got, _, err = store.GetUserSettings("settings_user")
		if err != nil {
			t.Fatalf("GetUserSettings after update failed: %v", err)
		}
		if got != want {
			t.Errorf("updated settings mismatch: got %+v, want %+v", got, want)
		}
	})

	t.Run("UpsertChatPreservesAndAutoHealsLastSeq", func(t *testing.T) {
		chatID := "dm_user1_user2"
		// 1. Initial chat creation with LastSeq 0
		if err := store.UpsertChat(models.Chat{ID: chatID, Name: chatID, IsDM: true, LastSeq: 0}); err != nil {
			t.Fatalf("UpsertChat failed: %v", err)
		}

		// 2. Add messages
		msg1 := models.Message{ChatID: chatID, Seq: 1, UserID: "user1", Content: "hello"}
		msg2 := models.Message{ChatID: chatID, Seq: 2, UserID: "user2", Content: "world"}
		if err := store.UpsertMessage(msg1); err != nil {
			t.Fatalf("UpsertMessage 1 failed: %v", err)
		}
		if err := store.UpsertMessage(msg2); err != nil {
			t.Fatalf("UpsertMessage 2 failed: %v", err)
		}

		// 3. UpsertChat with LastSeq 0 should NOT wipe existing LastSeq 2
		if err := store.UpsertChat(models.Chat{ID: chatID, Name: chatID, IsDM: true, LastSeq: 0}); err != nil {
			t.Fatalf("UpsertChat overwrite failed: %v", err)
		}

		chats, err := store.ListChats()
		if err != nil {
			t.Fatalf("ListChats failed: %v", err)
		}

		var targetChat *models.Chat
		for i := range chats {
			if chats[i].ID == chatID {
				targetChat = &chats[i]
				break
			}
		}

		if targetChat == nil {
			t.Fatalf("target chat %s not found in ListChats", chatID)
		}

		if targetChat.LastSeq != 2 {
			t.Errorf("expected LastSeq 2, got %d", targetChat.LastSeq)
		}
	})
}

func TestLegacyDBMessageMsgpackCompatibility(t *testing.T) {
	// Simulate legacy DBMessage without type and progress
	type LegacyDBMessage struct {
		Seq         int64          `msgpack:"seq"`
		Timestamp   int64          `msgpack:"timestamp"`
		ChatID      string         `msgpack:"chatId"`
		UserID      string         `msgpack:"userId"`
		Content     string         `msgpack:"content"`
		Attachments []DBAttachment `msgpack:"attachments"`
	}

	legacy := LegacyDBMessage{
		Seq:       1,
		Timestamp: 1700000000,
		ChatID:    "townhall",
		UserID:    "alice",
		Content:   "Legacy message",
	}

	data, err := msgpack.Marshal(&legacy)
	if err != nil {
		t.Fatalf("failed to marshal legacy message: %v", err)
	}

	var newMsg DBMessage
	if err := newMsg.UnmarshalBinary(data); err != nil {
		t.Fatalf("failed to unmarshal into new DBMessage: %v", err)
	}

	if newMsg.Seq != 1 || newMsg.Content != "Legacy message" {
		t.Errorf("unexpected decoded fields: %+v", newMsg)
	}
	if newMsg.Type != "" {
		t.Errorf("expected empty Type for legacy record, got %q", newMsg.Type)
	}
	if newMsg.Progress != nil {
		t.Errorf("expected nil Progress for legacy record, got %+v", newMsg.Progress)
	}
}

func TestProgressMessageStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_progress_test")
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

	chatID := "townhall"
	if err := store.UpsertChat(models.Chat{ID: chatID, Name: "Townhall", LastSeq: 0}); err != nil {
		t.Fatalf("UpsertChat failed: %v", err)
	}

	// 1. Insert root progress message
	rootMsg := models.Message{
		Seq:       10,
		Timestamp: 1700000010,
		ChatID:    chatID,
		UserID:    "bot1",
		Content:   "Compiling report...",
		Type:      models.MessageTypeProgress,
		Progress: &models.ProgressData{
			CardStatus: models.ProgressStatusRunning,
			Title:      "Compiling news report...",
		},
	}
	if err := store.UpsertMessage(rootMsg); err != nil {
		t.Fatalf("failed to insert root message: %v", err)
	}

	// 2. Insert child step 1 (running)
	child1 := models.Message{
		Seq:       11,
		Timestamp: 1700000011,
		ChatID:    chatID,
		UserID:    "bot1",
		Type:      models.MessageTypeProgress,
		Progress: &models.ProgressData{
			ParentSeq: 10,
			Step: &models.ProgressStep{
				ID:          "step-1",
				Title:       "Gathering preferences",
				Description: "Reading profile interests",
				Status:      models.ProgressStatusRunning,
			},
		},
	}
	if err := store.UpsertMessage(child1); err != nil {
		t.Fatalf("failed to insert child1: %v", err)
	}

	// 3. Insert child step 1 update (completed)
	child1Update := models.Message{
		Seq:       12,
		Timestamp: 1700000012,
		ChatID:    chatID,
		UserID:    "bot1",
		Type:      models.MessageTypeProgress,
		Progress: &models.ProgressData{
			ParentSeq: 10,
			Step: &models.ProgressStep{
				ID:          "step-1",
				Title:       "Gathering preferences",
				Description: "Reading profile interests",
				Status:      models.ProgressStatusCompleted,
			},
		},
	}
	if err := store.UpsertMessage(child1Update); err != nil {
		t.Fatalf("failed to insert child1Update: %v", err)
	}

	// 4. Insert child step 2 & complete entire card
	child2 := models.Message{
		Seq:       13,
		Timestamp: 1700000013,
		ChatID:    chatID,
		UserID:    "bot1",
		Type:      models.MessageTypeProgress,
		Progress: &models.ProgressData{
			ParentSeq:  10,
			CardStatus: models.ProgressStatusCompleted,
			Step: &models.ProgressStep{
				ID:     "step-2",
				Title:  "Fetching news sources",
				Status: models.ProgressStatusCompleted,
			},
		},
	}
	if err := store.UpsertMessage(child2); err != nil {
		t.Fatalf("failed to insert child2: %v", err)
	}

	// 5. Query ListMessages
	msgs, err := store.ListMessages(chatID, 1, 20)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}

	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}

	// Verify root message (msgs[0]) was atomically updated with the full projection snapshot!
	gotRoot := msgs[0]
	if gotRoot.Seq != 10 {
		t.Fatalf("expected first msg seq 10, got %d", gotRoot.Seq)
	}
	if gotRoot.Type != models.MessageTypeProgress {
		t.Errorf("expected root type %q, got %q", models.MessageTypeProgress, gotRoot.Type)
	}
	if gotRoot.Progress == nil {
		t.Fatalf("expected root progress to not be nil")
	}
	if gotRoot.Progress.CardStatus != models.ProgressStatusCompleted {
		t.Errorf("expected root card status completed, got %q", gotRoot.Progress.CardStatus)
	}
	if len(gotRoot.Progress.Steps) != 2 {
		t.Fatalf("expected root to have 2 steps, got %d", len(gotRoot.Progress.Steps))
	}
	if gotRoot.Progress.Steps[0].ID != "step-1" || gotRoot.Progress.Steps[0].Status != models.ProgressStatusCompleted {
		t.Errorf("unexpected step 1: %+v", gotRoot.Progress.Steps[0])
	}
	if gotRoot.Progress.Steps[1].ID != "step-2" || gotRoot.Progress.Steps[1].Status != models.ProgressStatusCompleted {
		t.Errorf("unexpected step 2: %+v", gotRoot.Progress.Steps[1])
	}
}

func TestProgressMessageStepTransitions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_step_transitions_test")
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

	chatID := "townhall"
	_ = store.UpsertChat(models.Chat{ID: chatID, Name: "Townhall", LastSeq: 0})

	// Root message with step 1 running
	_ = store.UpsertMessage(models.Message{
		Seq:       1,
		Timestamp: 100,
		ChatID:    chatID,
		UserID:    "bot1",
		Type:      models.MessageTypeProgress,
		Progress: &models.ProgressData{
			CardStatus: models.ProgressStatusRunning,
			Title:      "Task",
			Steps: []models.ProgressStep{
				{ID: "s1", Title: "Step 1", Status: models.ProgressStatusRunning},
			},
		},
	})

	// Append step 2 running -> step 1 should automatically become completed
	_ = store.UpsertMessage(models.Message{
		Seq:       2,
		Timestamp: 101,
		ChatID:    chatID,
		UserID:    "bot1",
		Type:      models.MessageTypeProgress,
		Progress: &models.ProgressData{
			ParentSeq: 1,
			Step: &models.ProgressStep{
				ID:     "s2",
				Title:  "Step 2",
				Status: models.ProgressStatusRunning,
			},
		},
	})

	msgs, _ := store.ListMessages(chatID, 1, 10)
	root := msgs[0]
	if len(root.Progress.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(root.Progress.Steps))
	}
	if root.Progress.Steps[0].Status != models.ProgressStatusCompleted {
		t.Errorf("expected step 1 to be completed after step 2 was appended, got %s", root.Progress.Steps[0].Status)
	}
	if root.Progress.Steps[1].Status != models.ProgressStatusRunning {
		t.Errorf("expected step 2 to be running, got %s", root.Progress.Steps[1].Status)
	}

	// Mark entire card completed -> step 2 should become completed
	_ = store.UpsertMessage(models.Message{
		Seq:       3,
		Timestamp: 102,
		ChatID:    chatID,
		UserID:    "bot1",
		Type:      models.MessageTypeProgress,
		Progress: &models.ProgressData{
			ParentSeq:  1,
			CardStatus: models.ProgressStatusCompleted,
		},
	})

	msgs, _ = store.ListMessages(chatID, 1, 10)
	root = msgs[0]
	if root.Progress.CardStatus != models.ProgressStatusCompleted {
		t.Errorf("expected card to be completed, got %s", root.Progress.CardStatus)
	}
	if root.Progress.Steps[0].Status != models.ProgressStatusCompleted {
		t.Errorf("expected step 1 to be completed, got %s", root.Progress.Steps[0].Status)
	}
	if root.Progress.Steps[1].Status != models.ProgressStatusCompleted {
		t.Errorf("expected step 2 to be completed when card completed, got %s", root.Progress.Steps[1].Status)
	}
}

