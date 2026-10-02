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

func TestUsersHandler_UserInfoAndAccessControl(t *testing.T) {
	apiInst, as, st, hub := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	// Add human user
	_, err := as.AddUser("alice", "Alice Human")
	require.NoError(t, err)
	aliceUser, err := as.GetUserByUsername("alice")
	require.NoError(t, err)
	require.NoError(t, as.ActivateUser(aliceUser.ID))

	// Add bot user
	botUser, botKey, err := as.AddBot("testbot", "Test Bot", models.BotPermissions{ReadAll: true})
	require.NoError(t, err)

	// Add webhook user
	_, webhookKey, err := as.AddWebhook("testwebhook", "Test Webhook", "townhall")
	require.NoError(t, err)

	// User connects and sends userInfo
	ch := hub.Join(aliceUser.ID)
	defer hub.Leave(aliceUser.ID, ch)

	enabled := true
	hub.Dispatch(aliceUser.ID, models.ClientMessage{
		Type:              models.ClientMessageTypeUserInfo,
		TimeZone:          "America/Chicago",
		PreferredLanguage: "en-US",
		SharingEnabled:    &enabled,
	}, ch)

	usersHandler := apiInst.RequireAuth(RequireUserTypes(apiInst.UsersHandler, models.UserTypeHuman, models.UserTypeBot))

	// 1. Webhook calling GET /api/users should be forbidden
	reqWebhook := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	reqWebhook.Header.Set("Authorization", "Bearer "+webhookKey)
	wWebhook := httptest.NewRecorder()
	usersHandler(wWebhook, reqWebhook)
	assert.Equal(t, http.StatusForbidden, wWebhook.Code)

	// 2. Bot calling GET /api/users should succeed
	reqBot := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	reqBot.Header.Set("Authorization", "Bearer "+botKey)
	wBot := httptest.NewRecorder()
	usersHandler(wBot, reqBot)
	assert.Equal(t, http.StatusOK, wBot.Code)
	assert.Equal(t, "private, no-store", wBot.Header().Get("Cache-Control"))

	var users []models.User
	require.NoError(t, json.Unmarshal(wBot.Body.Bytes(), &users))

	// Find Alice and verify TimeZone and PreferredLanguage
	var foundAlice, foundBot bool
	for _, u := range users {
		if u.ID == aliceUser.ID {
			foundAlice = true
			assert.True(t, u.Presence.Online)
			assert.Equal(t, "America/Chicago", u.TimeZone)
			assert.Equal(t, "en-US", u.PreferredLanguage)
		}
		if u.ID == botUser.ID {
			foundBot = true
			assert.Empty(t, u.TimeZone)
			assert.Empty(t, u.PreferredLanguage)
		}
	}
	assert.True(t, foundAlice)
	assert.True(t, foundBot)

	// 3. Confirm that auth storage / bbolt never persists TimeZone / PreferredLanguage
	storedAlice, err := as.GetUser(aliceUser.ID)
	require.NoError(t, err)
	assert.Empty(t, storedAlice.TimeZone)
	assert.Empty(t, storedAlice.PreferredLanguage)
}

func TestSendMessageHandler_ProgressMessage(t *testing.T) {
	apiInst, as, st, hub := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	_, err := as.AddUser("alice", "Alice Human")
	require.NoError(t, err)
	aliceUser, err := as.GetUserByUsername("alice")
	require.NoError(t, err)
	require.NoError(t, as.ActivateUser(aliceUser.ID))
	aliceKey, err := as.ResetAPIKey(aliceUser.ID)
	require.NoError(t, err)

	_, bot1Key, err := as.AddBot("bot1", "Bot 1", models.BotPermissions{Write: true, ReadAll: true})
	require.NoError(t, err)

	_, bot2Key, err := as.AddBot("bot2", "Bot 2", models.BotPermissions{Write: true, ReadAll: true})
	require.NoError(t, err)

	sendHandler := apiInst.RequireAuth(RequireSameOrigin(RequireUserTypes(apiInst.SendMessageHandler, models.UserTypeHuman, models.UserTypeBot)))
	getMessagesHandler := apiInst.RequireAuth(RequireUserTypes(apiInst.ChatMessagesHandler, models.UserTypeHuman, models.UserTypeBot))

	listenerCh := hub.Join("listener-id")
	defer hub.Leave("listener-id", listenerCh)

	// 1. Alice (human) attempts to send progress message -> 403 Forbidden
	progPayload, _ := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"title": "Should fail",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(progPayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+aliceKey)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	sendHandler(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 2. Conflicting type and messageType -> 400 Bad Request
	conflictPayload, _ := json.Marshal(map[string]any{
		"type":        "text",
		"messageType": "progress",
		"progress": map[string]any{
			"title": "Conflict",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(conflictPayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot1Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Unknown message type -> 400 Bad Request
	unknownPayload, _ := json.Marshal(map[string]any{
		"messageType": "alien",
		"content":     "test",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(unknownPayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot1Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 4. Text message with progress data -> 400 Bad Request
	textWithProgPayload, _ := json.Marshal(map[string]any{
		"messageType": "text",
		"content":     "hi",
		"progress": map[string]any{
			"title": "not allowed",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(textWithProgPayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot1Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 5. Bot1 creates root progress card -> 200 OK with seq and timestamp
	rootPayload, _ := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"title": "Indexing codebase",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(rootPayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot1Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rootResp struct {
		Seq       int64 `json:"seq"`
		Timestamp int64 `json:"timestamp"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rootResp))
	assert.Equal(t, int64(1), rootResp.Seq)
	assert.Positive(t, rootResp.Timestamp)

	// Verify broadcast on listener
	select {
	case sMsg := <-listenerCh:
		require.Equal(t, models.ServerMessageTypeMessages, sMsg.Type)
		require.Len(t, sMsg.Messages, 1)
		assert.Equal(t, models.MessageTypeProgress, sMsg.Messages[0].Type)
		assert.Equal(t, int64(1), sMsg.Messages[0].Seq)
		assert.Equal(t, "Indexing codebase", sMsg.Messages[0].Progress.Title)
		assert.Equal(t, models.ProgressStatusRunning, sMsg.Messages[0].Progress.CardStatus)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for root message broadcast")
	}

	// 6. Bot1 adds child step -> 200 OK with seq 2
	step1Payload, _ := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"parentSeq": 1,
			"step": map[string]any{
				"id":          "step1",
				"title":       "Scanning files",
				"description": "Looking for Go packages",
				"status":      "running",
			},
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(step1Payload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot1Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var step1Resp struct {
		Seq       int64 `json:"seq"`
		Timestamp int64 `json:"timestamp"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &step1Resp))
	assert.Equal(t, int64(2), step1Resp.Seq)

	// 7. Bot2 attempts to update parentSeq 1 (authored by Bot1) -> 403 Forbidden
	stepBot2Payload, _ := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"parentSeq": 1,
			"step": map[string]any{
				"id":     "step2",
				"title":  "Hijack attempt",
				"status": "running",
			},
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(stepBot2Payload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot2Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 8. Bot1 updates with non-existent parentSeq 999 -> 404 Not Found
	missingParentPayload, _ := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"parentSeq": 999,
			"step": map[string]any{
				"id":     "step1",
				"title":  "Missing",
				"status": "running",
			},
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(missingParentPayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot1Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 9. Bot1 completes the card
	donePayload, _ := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"parentSeq":  1,
			"cardStatus": "completed",
			"step": map[string]any{
				"id":     "step1",
				"title":  "Scanning files",
				"status": "completed",
			},
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(donePayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+bot1Key)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var doneResp struct {
		Seq       int64 `json:"seq"`
		Timestamp int64 `json:"timestamp"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doneResp))
	assert.Equal(t, int64(3), doneResp.Seq)

	// 10. Human posts a regular text message interleaved
	userTextPayload, _ := json.Marshal(map[string]any{
		"content": "Nice progress card!",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chats/townhall/messages", bytes.NewReader(userTextPayload))
	req.SetPathValue("id", "townhall")
	req.Header.Set("Authorization", "Bearer "+aliceKey)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sendHandler(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var userResp struct {
		Seq       int64 `json:"seq"`
		Timestamp int64 `json:"timestamp"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &userResp))
	assert.Equal(t, int64(4), userResp.Seq)

	// 11. Fetch messages via GET /api/chats/townhall/messages?toSeq=10
	reqFetch := httptest.NewRequest(http.MethodGet, "/api/chats/townhall/messages?toSeq=10", nil)
	reqFetch.SetPathValue("id", "townhall")
	reqFetch.Header.Set("Authorization", "Bearer "+aliceKey)
	wFetch := httptest.NewRecorder()
	getMessagesHandler(wFetch, reqFetch)
	require.Equal(t, http.StatusOK, wFetch.Code)

	var fetchedMessages []models.Message
	require.NoError(t, json.Unmarshal(wFetch.Body.Bytes(), &fetchedMessages))
	require.Len(t, fetchedMessages, 4)

	// Verify root message has hydrated snapshot with cardStatus=completed and 1 completed step
	rootFetched := fetchedMessages[0]
	assert.Equal(t, int64(1), rootFetched.Seq)
	assert.Equal(t, models.MessageTypeProgress, rootFetched.Type)
	require.NotNil(t, rootFetched.Progress)
	assert.Equal(t, "Indexing codebase", rootFetched.Progress.Title)
	assert.Equal(t, models.ProgressStatusCompleted, rootFetched.Progress.CardStatus)
	require.Len(t, rootFetched.Progress.Steps, 1)
	assert.Equal(t, "step1", rootFetched.Progress.Steps[0].ID)
	assert.Equal(t, models.ProgressStatusCompleted, rootFetched.Progress.Steps[0].Status)

	// Message 4 is normal text
	assert.Equal(t, int64(4), fetchedMessages[3].Seq)
	assert.Equal(t, "<p>Nice progress card!</p>\n", fetchedMessages[3].Content)
}

