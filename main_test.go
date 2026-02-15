package main

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration(t *testing.T) {
	// Setup temporary DB and ports
	dbFile := "integration_test.db"
	_ = os.Remove(dbFile) // cleanup before
	defer func() { _ = os.Remove(dbFile) }()

	adminAddr := "127.0.0.1:8882"
	apiAddr := ":8881"

	_ = os.Setenv("BESEDKA_DB", dbFile)
	_ = os.Setenv("ADMIN_ADDR", adminAddr)
	_ = os.Setenv("API_ADDR", apiAddr)
	_ = os.Setenv("AUTH_SECRET", "very-secure-test-secret")
	defer func() {
		_ = os.Unsetenv("BESEDKA_DB")
		_ = os.Unsetenv("ADMIN_ADDR")
		_ = os.Unsetenv("API_ADDR")
		_ = os.Unsetenv("AUTH_SECRET")
	}()

	// Start server in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := run(ctx); err != nil {
			// run returns context.Canceled on shutdown, ignore it
			if err != context.Canceled {
				t.Errorf("Server error: %v", err)
			}
		}
	}()

	// Wait for server to start
	waitForServer(t, "http://127.0.0.1:8882/admin/users", 20)

	// Step 0: Verify Root Redirect (New Check)
	// Requesting root without token should redirect to login.html with 302 Found
	{
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects automatically
			},
		}
		resp, err := client.Get(fmt.Sprintf("http://localhost%s/", apiAddr))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusFound, resp.StatusCode)
		location, err := resp.Location()
		require.NoError(t, err)
		require.Equal(t, "/login.html", location.Path)
	}

	// Step 1: Create User via Admin API (Invite)
	username := "testuser"
	reqBody, _ := json.Marshal(api.AddUserRequest{Username: username})
	resp, err := http.Post(fmt.Sprintf("http://%s/admin/users", adminAddr), "application/json", bytes.NewBuffer(reqBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var adminResp api.AddUserResponse
	err = json.NewDecoder(resp.Body).Decode(&adminResp)
	require.NoError(t, err)
	require.True(t, adminResp.Success)
	require.Equal(t, username, adminResp.Username)
	setupLink := adminResp.SetupLink
	require.NotEmpty(t, setupLink)

	// Extract token from setup link
	// Link format: /register.html?token=...
	parts := strings.Split(setupLink, "token=")
	require.Len(t, parts, 2)
	encodedToken := parts[1]
	require.NotEmpty(t, encodedToken)
	token, err := url.QueryUnescape(encodedToken)
	require.NoError(t, err)

	// Step 2: Get Registration Info
	// Use encodedToken because it's a URL query parameter
	resp, err = http.Get(fmt.Sprintf("http://localhost%s/api/register-info?token=%s", apiAddr, encodedToken))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var regInfo auth.RegistrationInfoResponse
	err = json.NewDecoder(resp.Body).Decode(&regInfo)
	require.NoError(t, err)
	require.Equal(t, username, regInfo.Username)
	require.NotEmpty(t, regInfo.TOTPSecret)
	totpSecret := regInfo.TOTPSecret

	// Step 3: Register (Complete Setup)
	newPassword := "newSecretPassword123"
	totpCode, err := auth.GenerateTOTP(totpSecret, time.Now())
	require.NoError(t, err)

	regReq := auth.RegistrationRequest{
		Token:       token,
		DisplayName: username + " Display",
		Password:    newPassword,
		TOTP:        totpCode,
	}
	regBody, _ := json.Marshal(regReq)
	resp, err = http.Post(fmt.Sprintf("http://localhost%s/api/register", apiAddr), "application/json", bytes.NewBuffer(regBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var regResp auth.RegistrationResponse
	err = json.NewDecoder(resp.Body).Decode(&regResp)
	require.NoError(t, err)
	require.True(t, regResp.Success)
	require.NotEmpty(t, regResp.Token)

	// Step 4: Login (Success with TOTP)
	totpCode, err = auth.GenerateTOTP(totpSecret, time.Now())
	require.NoError(t, err)

	loginReq2 := auth.LoginRequest{
		Username: username,
		Password: newPassword,
		TOTP:     totpCode,
	}
	loginBody2, _ := json.Marshal(loginReq2)
	resp, err = http.Post(fmt.Sprintf("http://localhost%s/api/login", apiAddr), "application/json", bytes.NewBuffer(loginBody2))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var loginResp auth.LoginResponse
	err = json.NewDecoder(resp.Body).Decode(&loginResp)
	require.NoError(t, err)
	require.True(t, loginResp.Success)
	require.NotEmpty(t, loginResp.Token)

	// Step 5: List Users
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	require.NoError(t, err)
	cookie := &http.Cookie{Name: "token", Value: loginResp.Token}
	req.AddCookie(cookie)

	client := &http.Client{}
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var usersList []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&usersList)
	require.NoError(t, err)

	// Check if our user is in the list
	found := false
	for _, u := range usersList {
		if u["userName"] == username {
			found = true
			break
		}
	}
	assert.True(t, found, "Newly created user %s should be in the users list. Got: %v", username, usersList)

	// Step 6: Multiple Tokens (New Check)
	// Login again to get a second token
	// Use time.Now().Add(30*time.Second) to generate a valid TOTP that is different from previous (to avoid reused TOTP error)
	totpCode, err = auth.GenerateTOTP(totpSecret, time.Now().Add(30*time.Second))
	require.NoError(t, err)

	loginReq3 := auth.LoginRequest{
		Username: username,
		Password: newPassword,
		TOTP:     totpCode,
	}
	loginBody3, _ := json.Marshal(loginReq3)
	resp, err = http.Post(fmt.Sprintf("http://localhost%s/api/login", apiAddr), "application/json", bytes.NewBuffer(loginBody3))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var loginResp2 auth.LoginResponse
	err = json.NewDecoder(resp.Body).Decode(&loginResp2)
	require.NoError(t, err)
	require.True(t, loginResp2.Success)
	require.NotEmpty(t, loginResp2.Token)
	require.NotEqual(t, loginResp.Token, loginResp2.Token, "Second login should return different token")

	// Verify FIRST token still works
	req1, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	req1.AddCookie(&http.Cookie{Name: "token", Value: loginResp.Token})
	client = &http.Client{} // reset client
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	defer func() { _ = resp1.Body.Close() }()
	require.Equal(t, http.StatusOK, resp1.StatusCode, "First token should still be valid")

	// Verify SECOND token works
	req2, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	req2.AddCookie(&http.Cookie{Name: "token", Value: loginResp2.Token})
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	require.Equal(t, http.StatusOK, resp2.StatusCode, "Second token should be valid")

	// Step 7: Admin Delete User Revokes Tokens (New Check)
	// Delete user via Admin API
	reqDel, _ := http.NewRequest("DELETE", fmt.Sprintf("http://%s/admin/users?username=%s", adminAddr, username), nil)
	respDel, err := client.Do(reqDel)
	require.NoError(t, err)
	defer func() { _ = respDel.Body.Close() }()
	require.Equal(t, http.StatusOK, respDel.StatusCode)

	// Verify FIRST token is revoked
	// API redirects to /login.html on auth failure (302) or returns 401 depending on endpoint/client config.
	// We configured client earlier to NOT follow redirects.
	// But here we use default client. Let's use custom client again.
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	reqRevoke1, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	reqRevoke1.AddCookie(&http.Cookie{Name: "token", Value: loginResp.Token})
	respRevoke1, err := noRedirectClient.Do(reqRevoke1)
	require.NoError(t, err)
	defer func() { _ = respRevoke1.Body.Close() }()
	// Should be 401 or 302 to login.
	// Internal auth middleware usually redirects to login on failure for browser routes, or 401 for API?
	// `besedka` seems to use `http.Redirect` for auth failure in `AuthMiddleware`?
	// Let's assume it redirects to `/login.html` (302).
	if respRevoke1.StatusCode != http.StatusUnauthorized && respRevoke1.StatusCode != http.StatusFound {
		t.Errorf("Expected 401 or 302 for revoked token 1, got %d", respRevoke1.StatusCode)
	}

	// Verify SECOND token is revoked
	reqRevoke2, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	reqRevoke2.AddCookie(&http.Cookie{Name: "token", Value: loginResp2.Token})
	respRevoke2, err := noRedirectClient.Do(reqRevoke2)
	require.NoError(t, err)
	defer func() { _ = respRevoke2.Body.Close() }()
	if respRevoke2.StatusCode != http.StatusUnauthorized && respRevoke2.StatusCode != http.StatusFound {
		t.Errorf("Expected 401 or 302 for revoked token 2, got %d", respRevoke2.StatusCode)
	}
}

func waitForServer(t *testing.T, url string, retries int) {
	for i := 0; i < retries; i++ {
		resp, err := http.Post(url, "application/json", bytes.NewBuffer([]byte("{}")))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusBadRequest {
				return
			}
			if resp.StatusCode == http.StatusNotFound {
				// Mux might not be ready or we hit wrong port?
				// Just retry a bit more in case of some race?
				// Or fail fast if we suspect configuration error.
				// But let's log it.
				fmt.Printf("WaitForServer: Got 404 from %s, retrying...\n", url)
			} else {
				// We got some response, assume server is up but maybe we sent bad request
				// fmt.Printf("WaitForServer: Got %d from %s\n", resp.StatusCode, url)
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Server failed to start at %s after %d retries", url, retries)
}
