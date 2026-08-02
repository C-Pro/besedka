package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/filestore"
	"besedka/internal/models"
	"besedka/internal/push"
	"besedka/internal/storage"
	"besedka/internal/ws"
)

func setupTestAPIServer(t *testing.T) (string, *auth.AuthService, *storage.BboltStorage, *ws.Hub, func()) {
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
		AuthSecret:   "test-secret-key-32-bytes-length!",
		MaxImageSize: 10 * 1024 * 1024,
		MaxFileSize:  25 * 1024 * 1024,
	}

	mockAssets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}

	apiSvc := NewAPIServer(cfg, as, hub, st, pushSvc, "127.0.0.1:0", mockAssets)
	testServer := httptest.NewServer(apiSvc.server.Handler)

	cleanup := func() {
		testServer.Close()
		_ = st.Close()
	}

	return testServer.URL, as, st, hub, cleanup
}

func TestWebhookPermissionsOnActualRoutes(t *testing.T) {
	tsURL, as, _, _, cleanup := setupTestAPIServer(t)
	defer cleanup()

	_, webhookKey, err := as.AddWebhook("wh_route_test", "Webhook Route Test", "townhall")
	if err != nil {
		t.Fatalf("AddWebhook failed: %v", err)
	}

	client := &http.Client{}

	// 1. Webhook posting to /api/webhook -> 200 OK
	body, _ := json.Marshal(map[string]string{"content": "Hello from webhook route!"})
	req, _ := http.NewRequest(http.MethodPost, tsURL+"/api/webhook", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+webhookKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/webhook failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for POST /api/webhook, got %d", resp.StatusCode)
	}

	// 2. Webhook reading messages GET /api/chats/townhall/messages?toSeq=10 -> 403 Forbidden
	reqGet, _ := http.NewRequest(http.MethodGet, tsURL+"/api/chats/townhall/messages?toSeq=10", nil)
	reqGet.Header.Set("Authorization", "Bearer "+webhookKey)
	respGet, err := client.Do(reqGet)
	if err != nil {
		t.Fatalf("GET /api/chats/townhall/messages failed: %v", err)
	}
	defer func() { _ = respGet.Body.Close() }()
	if respGet.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for webhook reading messages, got %d", respGet.StatusCode)
	}

	// 3. Webhook posting to chat API POST /api/chats/townhall/messages -> 403 Forbidden
	reqPostChat, _ := http.NewRequest(http.MethodPost, tsURL+"/api/chats/townhall/messages", bytes.NewReader(body))
	reqPostChat.Header.Set("Authorization", "Bearer "+webhookKey)
	reqPostChat.Header.Set("Content-Type", "application/json")
	respPostChat, err := client.Do(reqPostChat)
	if err != nil {
		t.Fatalf("POST /api/chats/townhall/messages failed: %v", err)
	}
	defer func() { _ = respPostChat.Body.Close() }()
	if respPostChat.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for webhook posting to chat API, got %d", respPostChat.StatusCode)
	}

	// 4. Webhook calling GET /api/me -> 200 OK
	reqMe, _ := http.NewRequest(http.MethodGet, tsURL+"/api/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+webhookKey)
	respMe, err := client.Do(reqMe)
	if err != nil {
		t.Fatalf("GET /api/me failed: %v", err)
	}
	defer func() { _ = respMe.Body.Close() }()
	if respMe.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for GET /api/me with webhook, got %d", respMe.StatusCode)
	}
}

func TestBotPermissionsOnActualRoutes(t *testing.T) {
	tsURL, as, _, _, cleanup := setupTestAPIServer(t)
	defer cleanup()

	// Bot with read_mentions only (Write = false)
	_, botKey, err := as.AddBot("bot_route_test", "Bot Route Test", models.BotPermissions{
		ReadMentions: true,
		Write:        false,
	})
	if err != nil {
		t.Fatalf("AddBot failed: %v", err)
	}

	client := &http.Client{}

	// 1. Bot posting to /api/webhook -> 403 Forbidden
	body, _ := json.Marshal(map[string]string{"content": "Hello from bot!"})
	reqWh, _ := http.NewRequest(http.MethodPost, tsURL+"/api/webhook", bytes.NewReader(body))
	reqWh.Header.Set("Authorization", "Bearer "+botKey)
	reqWh.Header.Set("Content-Type", "application/json")
	respWh, err := client.Do(reqWh)
	if err != nil {
		t.Fatalf("POST /api/webhook failed: %v", err)
	}
	defer func() { _ = respWh.Body.Close() }()
	if respWh.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for bot calling /api/webhook, got %d", respWh.StatusCode)
	}

	// 2. Bot posting without write permission POST /api/chats/townhall/messages -> 403 Forbidden
	reqPost, _ := http.NewRequest(http.MethodPost, tsURL+"/api/chats/townhall/messages", bytes.NewReader(body))
	reqPost.Header.Set("Authorization", "Bearer "+botKey)
	reqPost.Header.Set("Content-Type", "application/json")
	respPost, err := client.Do(reqPost)
	if err != nil {
		t.Fatalf("POST /api/chats/townhall/messages failed: %v", err)
	}
	defer func() { _ = respPost.Body.Close() }()
	if respPost.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for bot posting without write permission, got %d", respPost.StatusCode)
	}

	// 3. Bot reading messages GET /api/chats/townhall/messages?toSeq=10 -> 200 OK
	reqGet, _ := http.NewRequest(http.MethodGet, tsURL+"/api/chats/townhall/messages?toSeq=10", nil)
	reqGet.Header.Set("Authorization", "Bearer "+botKey)
	respGet, err := client.Do(reqGet)
	if err != nil {
		t.Fatalf("GET /api/chats/townhall/messages failed: %v", err)
	}
	defer func() { _ = respGet.Body.Close() }()
	if respGet.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for bot reading messages, got %d", respGet.StatusCode)
	}
}

func TestHumanPermissionsOnActualRoutes(t *testing.T) {
	tsURL, as, _, _, cleanup := setupTestAPIServer(t)
	defer cleanup()

	_, err := as.AddUser("human_route_user", "Human User")
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	humanUser, err := as.GetUserByUsername("human_route_user")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}

	if err := as.ActivateUser(humanUser.ID); err != nil {
		t.Fatalf("ActivateUser failed: %v", err)
	}

	humanKey, err := as.ResetAPIKey(humanUser.ID)
	if err != nil {
		t.Fatalf("ResetAPIKey failed: %v", err)
	}

	client := &http.Client{}

	// Human calling /api/webhook -> 403 Forbidden
	body, _ := json.Marshal(map[string]string{"content": "Human calling webhook endpoint"})
	reqWh, _ := http.NewRequest(http.MethodPost, tsURL+"/api/webhook", bytes.NewReader(body))
	reqWh.Header.Set("Authorization", "Bearer "+humanKey)
	reqWh.Header.Set("Content-Type", "application/json")
	respWh, err := client.Do(reqWh)
	if err != nil {
		t.Fatalf("POST /api/webhook failed: %v", err)
	}
	defer func() { _ = respWh.Body.Close() }()
	if respWh.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for human calling /api/webhook, got %d", respWh.StatusCode)
	}

	// Human posting to /api/chats/townhall/messages -> 200 OK
	reqPost, _ := http.NewRequest(http.MethodPost, tsURL+"/api/chats/townhall/messages", bytes.NewReader(body))
	reqPost.Header.Set("Authorization", "Bearer "+humanKey)
	reqPost.Header.Set("Content-Type", "application/json")
	respPost, err := client.Do(reqPost)
	if err != nil {
		t.Fatalf("POST /api/chats/townhall/messages failed: %v", err)
	}
	defer func() { _ = respPost.Body.Close() }()
	if respPost.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for human posting message, got %d", respPost.StatusCode)
	}
}
