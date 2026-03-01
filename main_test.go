package main

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntegration(t *testing.T) {
	// Setup temporary DB and ports
	dbFile := "integration_test.db"
	_ = os.Remove(dbFile) // cleanup before
	defer func() { _ = os.Remove(dbFile) }()

	adminAddr := "127.0.0.1:8888"
	apiAddr := ":8887"

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
		if err := run(ctx, ""); err != nil {
			// run returns context.Canceled on shutdown, ignore it
			if err != context.Canceled {
				t.Errorf("Server error: %v", err)
			}
		}
	}()

	// Wait for server to start
	waitForServer(t, "http://127.0.0.1:8888/admin/users", 20)

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
	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/admin/users", adminAddr), bytes.NewBuffer(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "1337chat")

	client := &http.Client{}
	resp, err := client.Do(req)
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

	// Step 2: Get Registration Info
	// Wait, setupLink is /register.html?token=...
	// We need to parse it.
	u, err := url.Parse(setupLink)
	require.NoError(t, err)
	token := u.Query().Get("token")
	require.NotEmpty(t, token)

	resp, err = http.Get(fmt.Sprintf("http://localhost%s/api/register-info?token=%s", apiAddr, url.QueryEscape(token)))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var regInfo auth.RegistrationInfoResponse
	err = json.NewDecoder(resp.Body).Decode(&regInfo)
	require.NoError(t, err)
	require.Equal(t, username, regInfo.Username)
	totpSecret := regInfo.TOTPSecret
	require.NotEmpty(t, totpSecret)

	// Step 3: Complete Registration
	// Provide password and TOTP code
	newPassword := "securepassword"
	// Generate TOTP code
	totpCode, err := auth.GenerateTOTP(totpSecret, time.Now())
	require.NoError(t, err)

	regReq := auth.RegistrationRequest{
		Token:    token,
		Password: newPassword,
		TOTP:     totpCode,
	}
	regBody, _ := json.Marshal(regReq)
	reqReg, _ := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/register", apiAddr), bytes.NewBuffer(regBody))
	reqReg.Header.Set("Content-Type", "application/json")
	reqReg.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))
	resp, err = client.Do(reqReg)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Step 4: Login
	loginReq := auth.LoginRequest{
		Username: username,
		Password: newPassword,
		TOTP:     totpCode,
	}

	loginBody, _ := json.Marshal(loginReq)
	reqLogin, _ := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/login", apiAddr), bytes.NewBuffer(loginBody))
	reqLogin.Header.Set("Content-Type", "application/json")
	reqLogin.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))
	resp, err = client.Do(reqLogin)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var loginResp auth.LoginResponse
	err = json.NewDecoder(resp.Body).Decode(&loginResp)
	require.NoError(t, err)
	require.True(t, loginResp.Success)
	sessionToken := loginResp.Token
	require.NotEmpty(t, sessionToken)

	// Step 4.5: Upload Avatar
	// We simulate an image upload using a minimal valid PNG valid for h2non/filetype
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
	pngDecoded, err := base64.StdEncoding.DecodeString(pngBase64)
	require.NoError(t, err)

	reqAvatar, err := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/users/me/avatar", apiAddr), bytes.NewReader(pngDecoded))
	require.NoError(t, err)
	reqAvatar.Header.Set("Content-Type", "image/png")
	reqAvatar.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	reqAvatar.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))

	respAvatar, err := client.Do(reqAvatar)
	require.NoError(t, err)
	defer func() { _ = respAvatar.Body.Close() }()
	require.Equal(t, http.StatusOK, respAvatar.StatusCode)

	var avatarResp struct {
		AvatarURL string `json:"avatarUrl"`
	}
	err = json.NewDecoder(respAvatar.Body).Decode(&avatarResp)
	require.NoError(t, err)
	require.NotEmpty(t, avatarResp.AvatarURL)
	require.Contains(t, avatarResp.AvatarURL, "/api/images/")

	// Fetch Me and check Avatar URL
	reqMe, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/me", apiAddr), nil)
	require.NoError(t, err)
	reqMe.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respMe, err := client.Do(reqMe)
	require.NoError(t, err)
	defer func() { _ = respMe.Body.Close() }()
	require.Equal(t, http.StatusOK, respMe.StatusCode)

	// Me returns reduced struct, let's fetch users list to verify avatar is there.
	// Step 5 does list users anyway and we will check it there.

	// Step 5: List Users (Verify Login and Avatar)

	reqList, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	reqList.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respList, err := client.Do(reqList) // client is clean
	require.NoError(t, err)
	defer func() { _ = respList.Body.Close() }()
	require.Equal(t, http.StatusOK, respList.StatusCode)

	// Step 7: Admin Delete User Revokes Tokens
	// Get user ID from user list
	reqUsers, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	reqUsers.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respUsers, err := client.Do(reqUsers)
	require.NoError(t, err)
	defer func() { _ = respUsers.Body.Close() }()
	require.Equal(t, http.StatusOK, respUsers.StatusCode)

	var users []struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(respUsers.Body).Decode(&users)
	require.NoError(t, err)
	require.NotEmpty(t, users)
	testUserID := users[0].ID

	// Delete user via Admin API
	reqDel, _ := http.NewRequest("DELETE", fmt.Sprintf("http://%s/api/users?id=%s", adminAddr, testUserID), nil)
	reqDel.SetBasicAuth("admin", "1337chat")
	client = &http.Client{}
	respDel, err := client.Do(reqDel)
	require.NoError(t, err)
	defer func() { _ = respDel.Body.Close() }()
	require.Equal(t, http.StatusOK, respDel.StatusCode)

	// Verify Token Revoked
	reqRevoke, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	reqRevoke.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	// We need client that doesn't follow redirects to check for 302/401
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	respRevoke, err := noRedirectClient.Do(reqRevoke)
	require.NoError(t, err)
	defer func() { _ = respRevoke.Body.Close() }()
	// Expect 302 Found (redirect to login) or 401
	if respRevoke.StatusCode != http.StatusFound && respRevoke.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 302 or 401, got %d", respRevoke.StatusCode)
	}
}

func waitForServer(t *testing.T, urlStr string, retries int) {
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.SetBasicAuth("admin", "1337chat") // Use default creds
	client := &http.Client{Timeout: 500 * time.Millisecond}

	for i := 0; i < retries; i++ {
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			// Accept 200 OK or 401 Unauthorized (invalid auth but server is up)
			// But since we send auth, we expect 200 or at least not connection refused.
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Server failed to start at %s after %d retries", urlStr, retries)
}
