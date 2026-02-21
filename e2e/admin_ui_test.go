package e2e

import (
	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/http"
	"besedka/internal/models"
	"besedka/internal/ws"
	"context"
	oshttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type mockStorage struct {
	users     map[string]auth.UserCredentials
	regTokens map[string]string
}

func (m *mockStorage) UpsertCredentials(c auth.UserCredentials) error {
	m.users[c.ID] = c
	return nil
}
func (m *mockStorage) ListCredentials() ([]auth.UserCredentials, error) {
	var list []auth.UserCredentials
	for _, c := range m.users {
		if c.Status == models.UserStatusActive {
			list = append(list, c)
		}
	}
	return list, nil
}
func (m *mockStorage) ListAllCredentials() ([]auth.UserCredentials, error) {
	var list []auth.UserCredentials
	for _, c := range m.users {
		list = append(list, c)
	}
	return list, nil
}
func (m *mockStorage) UpsertToken(uid, hash string) error     { return nil }
func (m *mockStorage) DeleteToken(hash string) error          { return nil }
func (m *mockStorage) ListTokens() (map[string]string, error) { return nil, nil }
func (m *mockStorage) UpsertRegistrationToken(uid, token string) error {
	m.regTokens[token] = uid
	return nil
}
func (m *mockStorage) DeleteRegistrationToken(uid string) error           { return nil }
func (m *mockStorage) ListRegistrationTokens() (map[string]string, error) { return m.regTokens, nil }
func (m *mockStorage) MigrateTokens(f func(string) string) error          { return nil }

// ws.storage implementation
func (m *mockStorage) UpsertMessage(message models.Message) error { return nil }
func (m *mockStorage) ListMessages(chatID string, from, to int64) ([]models.Message, error) {
	return nil, nil
}
func (m *mockStorage) ListChats() ([]models.Chat, error) { return nil, nil }
func (m *mockStorage) UpsertChat(chat models.Chat) error { return nil }

func TestAdminUI(t *testing.T) {
	// Setup dependencies
	cfg := &config.Config{
		AdminUser:     "admin",
		AdminPassword: "password",
		AdminAddr:     "localhost:0",
		AuthSecret:    "c2VjcmV0", // base64 "secret"
	}

	store := &mockStorage{
		users:     make(map[string]auth.UserCredentials),
		regTokens: make(map[string]string),
	}

	authService, err := auth.NewAuthService(context.Background(), auth.Config{
		Secret:      cfg.AuthSecret,
		TokenExpiry: time.Hour,
	}, store)
	if err != nil {
		t.Fatalf("Failed to create auth service: %v", err)
	}

	hub := ws.NewHub(authService, store) // Pass store to prevent panic

	adminServer := http.NewAdminServer(cfg, authService, hub)

	ts := httptest.NewServer(adminServer.Server().Handler)
	defer ts.Close()

	client := ts.Client()

	// 1. Test Unauthorized Access
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if resp.StatusCode != oshttp.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}

	// 2. Test Authorized Access (List Users - Empty)
	req, _ := oshttp.NewRequest("GET", ts.URL, nil)
	req.SetBasicAuth("admin", "password")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to get authorized: %v", err)
	}
	if resp.StatusCode != oshttp.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	// 3. Test Add User
	form := url.Values{}
	form.Add("username", "testuser")
	req, _ = oshttp.NewRequest("POST", ts.URL+"/admin/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "password")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to add user: %v", err)
	}
	if resp.StatusCode != oshttp.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	// Verify user exists in storage
	users, _ := authService.GetAllUsers()
	if len(users) != 1 || users[0].UserName != "testuser" {
		t.Errorf("Expected 1 user 'testuser', got %v", users)
	}

	// 4. Test Delete User
	form = url.Values{}
	form.Add("id", users[0].ID)
	req, _ = oshttp.NewRequest("POST", ts.URL+"/admin/users/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "password")

	// Go client follows redirects
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to delete user: %v", err)
	}
	if resp.StatusCode != oshttp.StatusOK {
		t.Errorf("Expected 200 (after redirect), got %d", resp.StatusCode)
	}

	// Verify user status is deleted
	users, _ = authService.GetAllUsers()
	if len(users) != 1 || users[0].Status != models.UserStatusDeleted {
		t.Errorf("Expected user status deleted, got %v", users[0].Status)
	}
}
