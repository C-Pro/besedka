package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"besedka/internal/auth"
	"besedka/internal/models"
	"testing/fstest"
)

type mockStorage struct {
	creds     map[string]auth.UserCredentials
	tokens    map[string]string // tokenHash -> userID
	regTokens map[string]string // userID -> token
}

func (m *mockStorage) UpsertCredentials(c auth.UserCredentials) error {
	m.creds[c.ID] = c
	return nil
}

func (m *mockStorage) ListCredentials() ([]auth.UserCredentials, error) {
	var list []auth.UserCredentials
	for _, c := range m.creds {
		if c.Status != "deleted" {
			list = append(list, c)
		}
	}
	return list, nil
}

func (m *mockStorage) ListAllCredentials() ([]auth.UserCredentials, error) {
	var list []auth.UserCredentials
	for _, c := range m.creds {
		list = append(list, c)
	}
	return list, nil
}

func (m *mockStorage) UpsertToken(userID string, tokenHash string) error {
	m.tokens[tokenHash] = userID
	return nil
}

func (m *mockStorage) DeleteToken(tokenHash string) error {
	delete(m.tokens, tokenHash)
	return nil
}

func (m *mockStorage) ListTokens() (map[string]string, error) {
	return m.tokens, nil
}

func (m *mockStorage) UpsertRegistrationToken(userID string, token string) error {
	m.regTokens[userID] = token
	return nil
}

func (m *mockStorage) DeleteRegistrationToken(userID string) error {
	delete(m.regTokens, userID)
	return nil
}

func (m *mockStorage) ListRegistrationTokens() (map[string]string, error) {
	return m.regTokens, nil
}

func hashToken(token string) string {
	h := hmac.New(sha512.New, []byte("server-secret"))
	h.Write([]byte(token))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func createTestAuthService(t *testing.T) (*auth.AuthService, *mockStorage) {
	cfg := auth.Config{
		Secret:      base64.StdEncoding.EncodeToString([]byte("server-secret")),
		TokenExpiry: time.Hour,
	}
	store := &mockStorage{
		creds:     make(map[string]auth.UserCredentials),
		tokens:    make(map[string]string),
		regTokens: make(map[string]string),
	}

	// Pre-populate credentials and session token
	store.creds["test-user-id"] = auth.UserCredentials{
		User: models.User{
			ID:       "test-user-id",
			UserName: "testuser",
			Status:   models.UserStatusActive,
		},
	}
	store.tokens[hashToken("test-token")] = "test-user-id"

	svc, err := auth.NewAuthService(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("failed to create auth service: %v", err)
	}
	return svc, store
}

func TestFileServerHeaders(t *testing.T) {
	authService, _ := createTestAuthService(t)
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("index page"),
		},
		"login.html": &fstest.MapFile{
			Data: []byte("login page"),
		},
		"sw.js": &fstest.MapFile{
			Data: []byte("sw content"),
		},
		"js/app.js": &fstest.MapFile{
			Data: []byte("console.log('hello');"),
		},
	}

	handler := NewFileServerHandler(authService, mockFS)

	tests := []struct {
		name             string
		path             string
		cookieToken      string
		expectedStatus   int
		expectedCache    string
		expectedRedirect string
	}{
		{
			name:           "Login HTML no cache",
			path:           "/login.html",
			expectedStatus: http.StatusOK,
			expectedCache:  "no-store, no-cache, must-revalidate, max-age=0",
		},
		{
			name:           "SW JS no cache",
			path:           "/sw.js",
			expectedStatus: http.StatusOK,
			expectedCache:  "no-cache",
		},
		{
			name:           "Static asset cached",
			path:           "/js/app.js",
			expectedStatus: http.StatusOK,
			expectedCache:  "public, max-age=31536000",
		},
		{
			name:             "Root redirect if unauthenticated",
			path:             "/",
			expectedStatus:   http.StatusFound,
			expectedRedirect: "/login.html",
		},
		{
			name:           "Root content if authenticated",
			path:           "/",
			cookieToken:    "test-token",
			expectedStatus: http.StatusOK,
			expectedCache:  "no-store, no-cache, must-revalidate, max-age=0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			if tc.cookieToken != "" {
				req.AddCookie(&http.Cookie{Name: "token", Value: tc.cookieToken})
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, rr.Code)
			}

			if tc.expectedRedirect != "" {
				loc := rr.Header().Get("Location")
				if loc != tc.expectedRedirect {
					t.Errorf("expected redirect location %q, got %q", tc.expectedRedirect, loc)
				}
			} else {
				cc := rr.Header().Get("Cache-Control")
				if cc != tc.expectedCache {
					t.Errorf("expected Cache-Control %q, got %q", tc.expectedCache, cc)
				}
			}
		})
	}
}
