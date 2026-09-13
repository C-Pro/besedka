package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/filestore"
	"besedka/internal/models"
	"besedka/internal/push"
	"besedka/internal/storage"
	"besedka/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAPIKeyTest(t *testing.T) (*API, *auth.AuthService, *storage.BboltStorage, *ws.Hub) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	uploadsPath := filepath.Join(dir, "uploads")

	fs, err := filestore.NewLocalFileStore(uploadsPath)
	if err != nil {
		t.Fatalf("failed to create filestore: %v", err)
	}

	st, err := storage.NewBboltStorage(dbPath, []byte("test-secret-key-32-bytes-length!"), fs)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	authCfg := auth.Config{
		Secret:        "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxlbmd0aCE=",
		RPDisplayName: "Test Chat",
		RPID:          "localhost",
		RPOrigin:      "http://localhost:8080",
	}

	as, err := auth.NewAuthService(context.Background(), authCfg, st)
	if err != nil {
		t.Fatalf("failed to create auth service: %v", err)
	}

	pushSvc, err := push.NewService(st)
	if err != nil {
		t.Fatalf("failed to create push service: %v", err)
	}

	hub := ws.NewHub(context.Background(), as, st, pushSvc)

	cfg := &config.Config{
		AuthSecret:    "test-secret-key-32-bytes-length!",
		MaxImageSize:  10 * 1024 * 1024,
		MaxAvatarSize: 5 * 1024 * 1024,
		MaxFileSize:   25 * 1024 * 1024,
	}

	apiInstance := New(as, hub, st, cfg, pushSvc)
	return apiInstance, as, st, hub
}

func TestWebhookHandler(t *testing.T) {
	apiInst, as, st, _ := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	_, webhookKey, err := as.AddWebhook("wh_test", "Webhook Test", "townhall")
	if err != nil {
		t.Fatalf("AddWebhook failed: %v", err)
	}

	// 1. Post to /api/webhook with Bearer token
	reqBody, _ := json.Marshal(map[string]string{"content": "Hello from webhook!"})
	req := httptest.NewRequest(http.MethodPost, "/api/webhook", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+webhookKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler := apiInst.RequireAuth(RequireSameOrigin(RequireUserTypes(apiInst.WebhookHandler, models.UserTypeWebhook)))
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil || !resp.Success {
		t.Errorf("expected success response, got %+v, err: %v", resp, err)
	}

	// 2. Webhook trying to read messages should be forbidden
	reqRead := httptest.NewRequest(http.MethodGet, "/api/chats/townhall/messages?toSeq=10", nil)
	reqRead.Header.Set("Authorization", "Bearer "+webhookKey)
	reqRead.SetPathValue("id", "townhall")

	wRead := httptest.NewRecorder()
	readHandler := apiInst.RequireAuth(RequireUserTypes(apiInst.ChatMessagesHandler, models.UserTypeHuman, models.UserTypeBot))
	readHandler(wRead, reqRead)

	if wRead.Code != http.StatusForbidden {
		t.Errorf("expected status 403 Forbidden for webhook reading messages, got %d", wRead.Code)
	}
}

func TestBotPermissionsInTownhall(t *testing.T) {
	apiInst, as, st, _ := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	// Create bot with ReadMentions only (no Write, no ReadAll)
	_, botKey, err := as.AddBot("mentionbot", "Mention Bot", models.BotPermissions{
		ReadMentions: true,
		ReadAll:      false,
		Write:        false,
	})
	if err != nil {
		t.Fatalf("AddBot failed: %v", err)
	}

	// Post message as bot should be forbidden (Write = false)
	reqBody, _ := json.Marshal(map[string]string{"content": "I should fail"})
	reqSend := httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(reqBody))
	reqSend.Header.Set("Authorization", "Bearer "+botKey)
	reqSend.SetPathValue("id", "townhall")

	wSend := httptest.NewRecorder()
	sendHandler := apiInst.RequireAuth(RequireSameOrigin(RequireUserTypes(apiInst.SendMessageHandler, models.UserTypeHuman, models.UserTypeBot)))
	sendHandler(wSend, reqSend)

	if wSend.Code != http.StatusForbidden {
		t.Errorf("expected status 403 Forbidden for bot posting without write permission, got %d", wSend.Code)
	}
}

func TestUploadAvatarHandler_BroadcastsNewUser(t *testing.T) {
	apiInst, as, st, hub := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	_, apiKey, err := as.AddBot("avataruser", "Avatar User", models.BotPermissions{
		Write: true,
	})
	require.NoError(t, err)

	listenerCh := hub.Join("listener-id")
	defer hub.Leave("listener-id", listenerCh)

	tinyPNG := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users/me/avatar", bytes.NewReader(tinyPNG))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "image/png")

	rec := httptest.NewRecorder()
	handler := apiInst.RequireAuth(apiInst.UploadAvatarHandler)
	handler(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		AvatarURL string `json:"avatarUrl"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.AvatarURL)

	select {
	case sMsg := <-listenerCh:
		assert.Equal(t, models.ServerMessageTypeNew, sMsg.Type)
		require.NotNil(t, sMsg.User)
		assert.Equal(t, resp.AvatarURL, sMsg.User.AvatarURL)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for avatar update broadcast")
	}
}

