package auth

import (
	"besedka/internal/models"
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestWebAuthnUser(t *testing.T) {
	storage := &MockStorage{
		creds:    make(map[string]UserCredentials),
		tokens:   make(map[string]string),
		passkeys: []Passkey{},
	}
	
	userCreds := UserCredentials{
		User: models.User{
			ID:          "user123",
			UserName:    "alice",
			DisplayName: "Alice",
		},
	}
	storage.creds["user123"] = userCreds

	ctx := context.Background()
	cfg := Config{
		Secret:        base64.StdEncoding.EncodeToString([]byte("very-secure-secret-key-for-development-mode")),
		TokenExpiry:   24 * time.Hour,
		RPDisplayName: "Besedka Test",
		RPID:          "localhost",
		RPOrigin:      "http://localhost",
	}
	
	as, err := NewAuthService(ctx, cfg, storage)
	if err != nil {
		t.Fatalf("failed to create AuthService: %v", err)
	}

	wUser, err := as.getWebAuthnUser("user123")
	if err != nil {
		t.Fatalf("failed to get webauthn user: %v", err)
	}

	if string(wUser.WebAuthnID()) != "user123" {
		t.Errorf("expected WebAuthnID to be user123, got %s", string(wUser.WebAuthnID()))
	}
	if wUser.WebAuthnName() != "alice" {
		t.Errorf("expected WebAuthnName to be alice, got %s", wUser.WebAuthnName())
	}
	if wUser.WebAuthnDisplayName() != "Alice" {
		t.Errorf("expected WebAuthnDisplayName to be Alice, got %s", wUser.WebAuthnDisplayName())
	}

	// Test credentials
	creds := wUser.WebAuthnCredentials()
	if len(creds) != 0 {
		t.Errorf("expected 0 credentials, got %d", len(creds))
	}

	// Add a passkey
	pk := Passkey{
		ID:        []byte("cred1"),
		UserID:    "user123",
		PublicKey: []byte("pubkey1"),
		Name:      "My Passkey",
	}
	storage.passkeys = append(storage.passkeys, pk)

	creds = wUser.WebAuthnCredentials()
	if len(creds) != 1 {
		t.Errorf("expected 1 credential, got %d", len(creds))
	} else {
		if string(creds[0].ID) != "cred1" {
			t.Errorf("expected credential ID to be cred1, got %s", string(creds[0].ID))
		}
	}
}

func TestWebAuthnSessions(t *testing.T) {
	storage := &MockStorage{
		creds:    make(map[string]UserCredentials),
		tokens:   make(map[string]string),
		passkeys: []Passkey{},
	}
	
	ctx := context.Background()
	cfg := Config{
		Secret:        base64.StdEncoding.EncodeToString([]byte("very-secure-secret-key-for-development-mode")),
		TokenExpiry:   24 * time.Hour,
		RPDisplayName: "Besedka Test",
		RPID:          "localhost",
		RPOrigin:      "http://localhost",
	}
	
	as, err := NewAuthService(ctx, cfg, storage)
	if err != nil {
		t.Fatalf("failed to create AuthService: %v", err)
	}

	sessionData := &webauthn.SessionData{
		Challenge: "test-challenge",
		UserID:    []byte("user123"),
	}

	as.SaveWebAuthnSession("sess1", sessionData)

	retrieved, ok := as.GetWebAuthnSession("sess1")
	if !ok {
		t.Fatal("expected session to be found")
	}
	if retrieved.Challenge != "test-challenge" {
		t.Errorf("expected challenge to be test-challenge, got %s", retrieved.Challenge)
	}

	as.DeleteWebAuthnSession("sess1")
	_, ok = as.GetWebAuthnSession("sess1")
	if ok {
		t.Error("expected session to be deleted")
	}
}

func TestBeginPasskeyRegistration(t *testing.T) {
	storage := &MockStorage{
		creds:    make(map[string]UserCredentials),
		tokens:   make(map[string]string),
		passkeys: []Passkey{},
	}
	
	// Register user
	userCreds := UserCredentials{
		User: models.User{
			ID:          "user123",
			UserName:    "alice",
			DisplayName: "Alice",
		},
	}
	storage.creds["user123"] = userCreds

	ctx := context.Background()
	cfg := Config{
		Secret:        base64.StdEncoding.EncodeToString([]byte("very-secure-secret-key-for-development-mode")),
		TokenExpiry:   24 * time.Hour,
		RPDisplayName: "Besedka Test",
		RPID:          "localhost",
		RPOrigin:      "http://localhost",
	}
	
	as, err := NewAuthService(ctx, cfg, storage)
	if err != nil {
		t.Fatalf("failed to create AuthService: %v", err)
	}

	options, session, err := as.BeginPasskeyRegistration("user123")
	if err != nil {
		t.Fatalf("failed to begin passkey registration: %v", err)
	}

	if options == nil || session == nil {
		t.Fatal("expected non-nil options and session")
	}
	if string(session.UserID) != "user123" {
		t.Errorf("expected session userID to be user123, got %s", string(session.UserID))
	}
}
