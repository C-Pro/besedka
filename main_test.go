package main

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration(t *testing.T) {
	// Setup temporary DB and ports
	dbFile := "integration_test.db"
	os.Remove(dbFile) // cleanup before
	defer os.Remove(dbFile)

	adminAddr := "localhost:8882"
	apiAddr := ":8881"

	os.Setenv("BESEDKA_DB", dbFile)
	os.Setenv("ADMIN_ADDR", adminAddr)
	os.Setenv("API_ADDR", apiAddr)
	defer func() {
		os.Unsetenv("BESEDKA_DB")
		os.Unsetenv("ADMIN_ADDR")
		os.Unsetenv("API_ADDR")
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
	waitForServer(t, "http://localhost:8882/admin/users", 20)

	// Step 1: Create User via Admin API
	username := "testuser"
	reqBody, _ := json.Marshal(api.AddUserRequest{Username: username})
	resp, err := http.Post(fmt.Sprintf("http://%s/admin/users", adminAddr), "application/json", bytes.NewBuffer(reqBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var adminResp api.AddUserResponse
	err = json.NewDecoder(resp.Body).Decode(&adminResp)
	require.NoError(t, err)
	require.True(t, adminResp.Success)
	require.Equal(t, username, adminResp.Username)
	tempPassword := adminResp.Password
	require.NotEmpty(t, tempPassword)

	// Step 2: Login (First Time) -> Expect NeedRegister = true
	loginReq := auth.LoginRequest{
		Username: username,
		Password: tempPassword,
		TOTP:     0,
	}
	loginBody, _ := json.Marshal(loginReq)
	resp, err = http.Post(fmt.Sprintf("http://localhost%s/api/login", apiAddr), "application/json", bytes.NewBuffer(loginBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Step 3: Register (Complete Setup)
	newPassword := "newSecretPassword123"
	regReq := auth.RegistrationRequest{
		Username:    username,
		Password:    tempPassword,
		NewPassword: newPassword,
	}
	regBody, _ := json.Marshal(regReq)
	resp, err = http.Post(fmt.Sprintf("http://localhost%s/api/register", apiAddr), "application/json", bytes.NewBuffer(regBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var regResp auth.RegistrationResponse
	err = json.NewDecoder(resp.Body).Decode(&regResp)
	require.NoError(t, err)
	require.True(t, regResp.Success)
	require.NotEmpty(t, regResp.TOTPSecret)

	// Step 4: Login (Success with TOTP)
	totpCode, err := auth.GenerateTOTP(regResp.TOTPSecret, time.Now())
	require.NoError(t, err)

	loginReq2 := auth.LoginRequest{
		Username: username,
		Password: newPassword,
		TOTP:     totpCode,
	}
	loginBody2, _ := json.Marshal(loginReq2)
	resp, err = http.Post(fmt.Sprintf("http://localhost%s/api/login", apiAddr), "application/json", bytes.NewBuffer(loginBody2))
	require.NoError(t, err)
	defer resp.Body.Close()
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
	defer resp.Body.Close()
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
}

func waitForServer(t *testing.T, url string, retries int) {
	for i := 0; i < retries; i++ {
		resp, err := http.Post(url, "application/json", bytes.NewBuffer([]byte("{}")))
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Server failed to start at %s after %d retries", url, retries)
}
