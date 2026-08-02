package auth

import (
	"context"
	"testing"

	"besedka/internal/models"
)

func newTestAuthService(t *testing.T) *AuthService {
	t.Helper()

	mockSt := &MockStorage{
		creds:     make(map[string]UserCredentials),
		tokens:    make(map[string]string),
		regTokens: make(map[string]string),
		apiKeys:   make(map[string]string),
	}

	cfg := Config{
		Secret:        "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxlbmd0aCE=", // base64 of secret
		RPDisplayName: "Test Chat",
		RPID:          "localhost",
		RPOrigin:      "http://localhost:8080",
	}

	as, err := NewAuthService(context.Background(), cfg, mockSt)
	if err != nil {
		t.Fatalf("failed to create auth service: %v", err)
	}

	return as
}

func TestAddBotAndAPIKeyAuth(t *testing.T) {
	as := newTestAuthService(t)

	perms := models.BotPermissions{
		ReadMentions: true,
		ReadAll:      false,
		Write:        true,
	}

	botUser, apiKey, err := as.AddBot("testbot", "Test Bot", perms)
	if err != nil {
		t.Fatalf("AddBot failed: %v", err)
	}

	if botUser.Type != models.UserTypeBot {
		t.Errorf("expected user type %s, got %s", models.UserTypeBot, botUser.Type)
	}
	if botUser.Status != models.UserStatusActive {
		t.Errorf("expected user status %s, got %s", models.UserStatusActive, botUser.Status)
	}
	if botUser.BotPermissions != perms {
		t.Errorf("expected bot permissions %+v, got %+v", perms, botUser.BotPermissions)
	}
	if apiKey == "" {
		t.Fatal("expected non-empty API key")
	}

	// Verify authentication using API key
	foundUser, err := as.GetUserByAPIKey(apiKey)
	if err != nil {
		t.Fatalf("GetUserByAPIKey failed: %v", err)
	}
	if foundUser.ID != botUser.ID {
		t.Errorf("expected user ID %s, got %s", botUser.ID, foundUser.ID)
	}

	// Verify reset API key
	newKey, err := as.ResetAPIKey(botUser.ID)
	if err != nil {
		t.Fatalf("ResetAPIKey failed: %v", err)
	}
	if newKey == apiKey {
		t.Errorf("expected new API key, got identical key")
	}

	// Old key should fail
	if _, err := as.GetUserByAPIKey(apiKey); err == nil {
		t.Errorf("expected old API key to fail authentication")
	}

	// New key should succeed
	if foundUser, err = as.GetUserByAPIKey(newKey); err != nil || foundUser.ID != botUser.ID {
		t.Errorf("expected new API key authentication to succeed, got user %v, err %v", foundUser, err)
	}
}

func TestAddWebhook(t *testing.T) {
	as := newTestAuthService(t)

	// 1. Townhall target
	webhookUser, apiKey, err := as.AddWebhook("mywebhook", "My Webhook", "townhall")
	if err != nil {
		t.Fatalf("AddWebhook failed: %v", err)
	}

	if webhookUser.Type != models.UserTypeWebhook {
		t.Errorf("expected user type %s, got %s", models.UserTypeWebhook, webhookUser.Type)
	}
	if webhookUser.TargetChatID != "townhall" {
		t.Errorf("expected target chat townhall, got %s", webhookUser.TargetChatID)
	}

	foundUser, err := as.GetUserByAPIKey(apiKey)
	if err != nil {
		t.Fatalf("GetUserByAPIKey failed: %v", err)
	}
	if foundUser.ID != webhookUser.ID {
		t.Errorf("expected user ID %s, got %s", webhookUser.ID, foundUser.ID)
	}

	// 2. Target nonexistent user should fail validation
	if _, _, err := as.AddWebhook("invalidwh", "Invalid Webhook", "unknown_user"); err == nil {
		t.Errorf("expected error for nonexistent target user")
	}

	// 3. Target valid user username
	if _, err := as.AddUser("alice", "Alice"); err != nil {
		t.Fatalf("failed to create alice: %v", err)
	}
	aliceUser, err := as.GetUserByUsername("alice")
	if err != nil {
		t.Fatalf("failed to get alice user: %v", err)
	}
	aliceID := aliceUser.ID

	dmWebhook, _, err := as.AddWebhook("alicewebhook", "Alice Webhook", "alice")
	if err != nil {
		t.Fatalf("AddWebhook for alice target failed: %v", err)
	}

	expectedDMID := models.GetDMID(dmWebhook.ID, aliceID)
	if dmWebhook.TargetChatID != expectedDMID {
		t.Errorf("expected target chat %s, got %s", expectedDMID, dmWebhook.TargetChatID)
	}
}
