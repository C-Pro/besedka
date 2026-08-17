package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/filestore"
	"besedka/internal/models"
	"besedka/internal/push"
	"besedka/internal/storage"
	"besedka/internal/ws"

	"github.com/gorilla/websocket"
)

type wsReader struct {
	conn *websocket.Conn
	ch   chan models.ServerMessage
	err  chan error
}

func newWSReader(conn *websocket.Conn) *wsReader {
	r := &wsReader{
		conn: conn,
		ch:   make(chan models.ServerMessage, 100),
		err:  make(chan error, 1),
	}
	go func() {
		for {
			var msg models.ServerMessage
			if err := conn.ReadJSON(&msg); err != nil {
				r.err <- err
				return
			}
			r.ch <- msg
		}
	}()
	return r
}

func (r *wsReader) waitForMessage(timeout time.Duration, predicate func(m models.Message) bool) bool {
	deadline := time.After(timeout)
	for {
		select {
		case sMsg := <-r.ch:
			if sMsg.Type == models.ServerMessageTypeMessages {
				for _, m := range sMsg.Messages {
					if predicate(m) {
						return true
					}
				}
			}
		case <-deadline:
			return false
		case <-r.err:
			return false
		}
	}
}

func TestBotAndWebhookHappyPathIntegration(t *testing.T) {
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
	defer func() { _ = st.Close() }()

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
		AdminUser:     "admin",
		AdminPassword: "adminpassword",
		MaxImageSize:  10 * 1024 * 1024,
		MaxFileSize:   25 * 1024 * 1024,
		ChatName:      "Besedka Test",
	}

	mockAssets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
		"admin.html": &fstest.MapFile{Data: []byte("<html>{{.ChatName}}</html>")},
	}

	apiSvc := NewAPIServer(cfg, as, hub, st, pushSvc, "127.0.0.1:0", mockAssets)
	apiServer := httptest.NewServer(apiSvc.server.Handler)
	defer apiServer.Close()

	adminSvc := NewAdminServer(cfg, as, hub, st, mockAssets)
	adminServer := httptest.NewServer(adminSvc.server.Handler)
	defer adminServer.Close()

	client := &http.Client{}

	// --- 1. Admin API: Register Bot ---
	botReqBody, _ := json.Marshal(api.AddUserRequest{
		Username:    "integ_bot",
		DisplayName: "Integration Bot",
		Type:        string(models.UserTypeBot),
		BotPermissions: models.BotPermissions{
			ReadMentions: true,
			Write:        true,
		},
	})
	reqAddBot, _ := http.NewRequest(http.MethodPost, adminServer.URL+"/admin/users", bytes.NewReader(botReqBody))
	reqAddBot.SetBasicAuth("admin", "adminpassword")
	reqAddBot.Header.Set("Content-Type", "application/json")

	respAddBot, err := client.Do(reqAddBot)
	if err != nil {
		t.Fatalf("Admin AddBot request failed: %v", err)
	}
	var botAddResp api.AddUserResponse
	_ = json.NewDecoder(respAddBot.Body).Decode(&botAddResp)
	_ = respAddBot.Body.Close()

	if !botAddResp.Success || botAddResp.APIKey == "" {
		t.Fatalf("Failed to create bot via admin API: %+v", botAddResp)
	}
	botAPIKey := botAddResp.APIKey

	// --- 2. Admin API: Register Human User ---
	humanReqBody, _ := json.Marshal(api.AddUserRequest{
		Username:    "integ_human",
		DisplayName: "Integration Human",
	})
	reqAddHuman, _ := http.NewRequest(http.MethodPost, adminServer.URL+"/admin/users", bytes.NewReader(humanReqBody))
	reqAddHuman.SetBasicAuth("admin", "adminpassword")
	reqAddHuman.Header.Set("Content-Type", "application/json")

	respAddHuman, err := client.Do(reqAddHuman)
	if err != nil {
		t.Fatalf("Admin AddHuman request failed: %v", err)
	}
	var humanAddResp api.AddUserResponse
	_ = json.NewDecoder(respAddHuman.Body).Decode(&humanAddResp)
	_ = respAddHuman.Body.Close()

	if !humanAddResp.Success {
		t.Fatalf("Failed to create human via admin API: %+v", humanAddResp)
	}

	humanUser, err := as.GetUserByUsername("integ_human")
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

	// --- 3. Connect Bot via WebSocket ---
	wsURL := "ws" + strings.TrimPrefix(apiServer.URL, "http") + "/api/chat"
	botHeader := http.Header{}
	botHeader.Set("Authorization", "Bearer "+botAPIKey)

	botWSConn, _, err := websocket.DefaultDialer.Dial(wsURL, botHeader)
	if err != nil {
		t.Fatalf("Bot websocket dial failed: %v", err)
	}
	defer func() { _ = botWSConn.Close() }()
	botReader := newWSReader(botWSConn)

	// --- 4. Connect Human via WebSocket ---
	humanHeader := http.Header{}
	humanHeader.Set("Authorization", "Bearer "+humanKey)

	humanWSConn, _, err := websocket.DefaultDialer.Dial(wsURL, humanHeader)
	if err != nil {
		t.Fatalf("Human websocket dial failed: %v", err)
	}
	defer func() { _ = humanWSConn.Close() }()
	humanReader := newWSReader(humanWSConn)

	// Allow connections to be joined into hub chats
	time.Sleep(50 * time.Millisecond)

	// --- 5. Human sends message mentioning bot ---
	msgMention := models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  "townhall",
		Content: "Hello @integ_bot!",
	}
	if err := humanWSConn.WriteJSON(msgMention); err != nil {
		t.Fatalf("Human failed to send mention message: %v", err)
	}

	gotMention := botReader.waitForMessage(2*time.Second, func(m models.Message) bool {
		return strings.Contains(m.Content, "@integ_bot")
	})
	if !gotMention {
		t.Errorf("Bot expected to receive mentioned message")
	}

	// --- 6. Human sends unmentioned message ---
	msgNoMention := models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  "townhall",
		Content: "General message without mention",
	}
	if err := humanWSConn.WriteJSON(msgNoMention); err != nil {
		t.Fatalf("Human failed to send unmentioned message: %v", err)
	}

	gotUnmentioned := botReader.waitForMessage(300*time.Millisecond, func(m models.Message) bool {
		return strings.Contains(m.Content, "General message without mention")
	})
	if gotUnmentioned {
		t.Errorf("Bot with ReadMentions received an unmentioned message!")
	}

	// --- 7. Bot sends message via WebSocket ---
	botSendMsg := models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  "townhall",
		Content: "Beep boop from bot",
	}
	if err := botWSConn.WriteJSON(botSendMsg); err != nil {
		t.Fatalf("Bot failed to send message: %v", err)
	}

	gotBotMsg := humanReader.waitForMessage(2*time.Second, func(m models.Message) bool {
		return strings.Contains(m.Content, "Beep boop from bot")
	})
	if !gotBotMsg {
		t.Errorf("Human expected to receive message sent by bot")
	}

	// --- 8. Admin API: Register Webhook ---
	whReqBody, _ := json.Marshal(api.AddUserRequest{
		Username:    "integ_wh",
		DisplayName: "Integration Webhook",
		Type:        string(models.UserTypeWebhook),
		Target:      "townhall",
	})
	reqAddWh, _ := http.NewRequest(http.MethodPost, adminServer.URL+"/admin/users", bytes.NewReader(whReqBody))
	reqAddWh.SetBasicAuth("admin", "adminpassword")
	reqAddWh.Header.Set("Content-Type", "application/json")

	respAddWh, err := client.Do(reqAddWh)
	if err != nil {
		t.Fatalf("Admin AddWebhook request failed: %v", err)
	}
	var whAddResp api.AddUserResponse
	_ = json.NewDecoder(respAddWh.Body).Decode(&whAddResp)
	_ = respAddWh.Body.Close()

	if !whAddResp.Success || whAddResp.APIKey == "" {
		t.Fatalf("Failed to create webhook via admin API: %+v", whAddResp)
	}
	webhookAPIKey := whAddResp.APIKey

	// --- 9. Post message via Webhook endpoint ---
	whPayload, _ := json.Marshal(map[string]string{"content": "Automated deployment payload"})
	reqPostWh, _ := http.NewRequest(http.MethodPost, apiServer.URL+"/api/webhook", bytes.NewReader(whPayload))
	reqPostWh.Header.Set("Authorization", "Bearer "+webhookAPIKey)
	reqPostWh.Header.Set("Content-Type", "application/json")

	respPostWh, err := client.Do(reqPostWh)
	if err != nil {
		t.Fatalf("POST /api/webhook failed: %v", err)
	}
	_ = respPostWh.Body.Close()

	if respPostWh.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for POST /api/webhook, got %d", respPostWh.StatusCode)
	}

	gotWebhookMsg := humanReader.waitForMessage(2*time.Second, func(m models.Message) bool {
		return strings.Contains(m.Content, "Automated deployment payload")
	})
	if !gotWebhookMsg {
		t.Errorf("Human expected to receive message posted via webhook")
	}
}
