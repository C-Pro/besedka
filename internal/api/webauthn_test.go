package api

import (
	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/filestore"
	"besedka/internal/storage"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// We can define a helper to setup a test API
func setupTestAPI(t *testing.T) (*API, func()) {
	tmpDir, err := os.MkdirTemp(".", "api_test_")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	fs, err := filestore.NewLocalFileStore(filepath.Join(tmpDir, "uploads"))
	if err != nil {
		cleanup()
		t.Fatalf("failed to create local file store: %v", err)
	}

	authSecret := "very-secure-secret-key-for-development-mode"
	bbStorage, err := storage.NewBboltStorage(dbPath, []byte(authSecret), fs)
	if err != nil {
		cleanup()
		t.Fatalf("failed to create bbolt storage: %v", err)
	}

	authConfig := auth.Config{
		Secret:        base64.StdEncoding.EncodeToString([]byte(authSecret)),
		TokenExpiry:   24 * time.Hour,
		RPDisplayName: "Besedka Test",
		RPID:          "localhost",
		RPOrigin:      "http://localhost",
	}

	ctx := context.Background()
	authService, err := auth.NewAuthService(ctx, authConfig, bbStorage)
	if err != nil {
		_ = bbStorage.Close()
		cleanup()
		t.Fatalf("failed to create auth service: %v", err)
	}

	cfg := &config.Config{
		BaseURL: "http://localhost",
	}

	api := New(authService, nil, bbStorage, cfg, nil)

	return api, func() {
		_ = bbStorage.Close()
		cleanup()
	}
}

func TestListPasskeysHandler(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Call ListPasskeysHandler with context containing UserID
	req := httptest.NewRequest("GET", "/api/webauthn/passkeys", nil)
	w := httptest.NewRecorder()
	
	ctx := context.WithValue(req.Context(), userIDKey, "user123")
	req = req.WithContext(ctx)

	api.ListPasskeysHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var passkeys []passkeyJSON
	if err := json.Unmarshal(w.Body.Bytes(), &passkeys); err != nil {
		t.Fatalf("failed to unmarshal passkeys response: %v", err)
	}
	if len(passkeys) != 0 {
		t.Errorf("expected 0 passkeys, got %d", len(passkeys))
	}
}

func TestDeletePasskeyHandler(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// 1. Create a dummy passkey in storage for the user
	credID := []byte("test-credential-id")
	pk := auth.Passkey{
		ID:        credID,
		UserID:    "user123",
		PublicKey: []byte("pubkey"),
		Name:      "Test Passkey",
	}
	
	err := api.storage.UpsertPasskey(pk)
	if err != nil {
		t.Fatalf("failed to insert test passkey: %v", err)
	}

	// Verify it is there
	pks, err := api.storage.ListPasskeys("user123")
	if err != nil || len(pks) != 1 {
		t.Fatalf("expected 1 passkey, got %d, err: %v", len(pks), err)
	}

	// 2. Call DeletePasskeyHandler
	idB64 := base64.RawURLEncoding.EncodeToString(credID)
	req := httptest.NewRequest("DELETE", "/api/webauthn/passkeys/"+idB64, nil)
	req.SetPathValue("id", idB64)

	w := httptest.NewRecorder()
	ctx := context.WithValue(req.Context(), userIDKey, "user123")
	req = req.WithContext(ctx)

	api.DeletePasskeyHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify it was deleted from storage
	pks, err = api.storage.ListPasskeys("user123")
	if err != nil || len(pks) != 0 {
		t.Errorf("expected 0 passkeys after deletion, got %d", len(pks))
	}
}

func TestDeletePasskeyHandler_InvalidID(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/api/webauthn/passkeys/invalid-b64-!!!", nil)
	req.SetPathValue("id", "invalid-b64-!!!")

	w := httptest.NewRecorder()
	ctx := context.WithValue(req.Context(), userIDKey, "user123")
	req = req.WithContext(ctx)

	api.DeletePasskeyHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestDeletePasskeyHandler_URLSafeBase64(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Credential ID with bytes that produce +, /, = in standard base64
	// but are safe with base64url encoding.
	credID := []byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa}
	stdB64 := base64.StdEncoding.EncodeToString(credID)
	urlB64 := base64.RawURLEncoding.EncodeToString(credID)
	if stdB64 == urlB64 {
		t.Fatal("test setup: credID should produce different std vs url base64")
	}

	pk := auth.Passkey{
		ID:        credID,
		UserID:    "user123",
		PublicKey: []byte("pubkey"),
		Name:      "Slash Passkey",
	}
	if err := api.storage.UpsertPasskey(pk); err != nil {
		t.Fatalf("failed to insert passkey: %v", err)
	}

	// List and verify the returned ID is base64url
	req := httptest.NewRequest("GET", "/api/webauthn/passkeys", nil)
	w := httptest.NewRecorder()
	ctx := context.WithValue(req.Context(), userIDKey, "user123")
	req = req.WithContext(ctx)
	api.ListPasskeysHandler(w, req)

	var listed []passkeyJSON
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != urlB64 {
		t.Fatalf("expected ID %q, got %q", urlB64, listed[0].ID)
	}

	// Delete using the base64url ID from the list response
	req = httptest.NewRequest("DELETE", "/api/webauthn/passkeys/"+listed[0].ID, nil)
	req.SetPathValue("id", listed[0].ID)
	w = httptest.NewRecorder()
	ctx = context.WithValue(req.Context(), userIDKey, "user123")
	req = req.WithContext(ctx)
	api.DeletePasskeyHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	pks, _ := api.storage.ListPasskeys("user123")
	if len(pks) != 0 {
		t.Errorf("expected 0 passkeys, got %d", len(pks))
	}
}

func TestDeletePasskeyHandler_EmptyID(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/api/webauthn/passkeys/", nil)
	req.SetPathValue("id", "")

	w := httptest.NewRecorder()
	ctx := context.WithValue(req.Context(), userIDKey, "user123")
	req = req.WithContext(ctx)

	api.DeletePasskeyHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
