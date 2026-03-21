package internal

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"besedka/internal/auth"
	"besedka/internal/filestore"
	"besedka/internal/models"
	"besedka/internal/storage"
)

func TestMigrationTool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "migrate_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := filepath.Join(tmpDir, "migrate.db")
	fsPath := filepath.Join(tmpDir, "fs")

	// 1. Create UNENCRYPTED database by passing nil key
	fs, err := filestore.NewLocalFileStore(fsPath)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}
	store, err := storage.NewBboltStorage(dbPath, nil, fs)
	if err != nil {
		t.Fatalf("failed to create unencrypted DB: %v", err)
	}

	creds := auth.UserCredentials{
		User:         models.User{ID: "enc_user", Status: models.UserStatusActive},
		PasswordHash: "hash123",
	}
	if err := store.UpsertCredentials(creds); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertChat(models.Chat{ID: "townhall"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMessage(models.Message{ChatID: "townhall", Seq: 1, Content: "unencrypted-msg"}); err != nil {
		t.Fatal(err)
	}

	// Add file
	content := []byte("plain text content")
	hash := "somehash"
	if err := store.SaveFileBlob(bytes.NewReader(content), hash); err != nil {
		t.Fatal(err)
	}
	meta := storage.FileMetadata{ID: "f1", Hash: hash}
	if err := store.UpsertFileMetadata(meta); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	} // Now we have an unencrypted DB

	// 2. Run Migration Tool
	key := []byte("12345678901234567890123456789012") // 32 bytes
	if err := MigrateToEncryption(dbPath, fsPath, key); err != nil {
		t.Fatalf("MigrateToEncryption failed: %v", err)
	}

	// 3. Open migrated DB with key
	storeEnc, err := storage.NewBboltStorage(dbPath, key, fs)
	if err != nil {
		t.Fatalf("Failed to open encrypted, migrated DB: %v", err)
	}

	// Verify User
	readCreds, err := storeEnc.ListCredentials()
	if err != nil || len(readCreds) != 1 || readCreds[0].PasswordHash != "hash123" {
		t.Fatalf("failed to read migrated credentials: %v", err)
	}

	// Verify Message
	msgs, err := storeEnc.ListMessages("townhall", 0, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "unencrypted-msg" {
		t.Fatalf("failed to read migrated messages: %v", msgs)
	}

	// Verify File Content
	rc, err := storeEnc.GetFileBlob(hash)
	if err != nil {
		t.Fatal(err)
	}
	read, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(read) != string(content) {
		t.Fatalf("after migration expected %s, got %s", content, read)
	}

	// Read literal disk file, it should NOT be plaintext
	diskPath := filepath.Join(tmpDir, "fs", hash[:2], hash)
	diskData, _ := os.ReadFile(diskPath)
	if string(diskData) == string(content) {
		t.Fatalf("file content on disk should be encrypted")
	}

	_ = storeEnc.Close()
}
