package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationUI(t *testing.T) {
	// Setup temporary DB and ports
	dbFile := "integration_ui_test.db"
	_ = os.Remove(dbFile) // cleanup before
	defer func() { _ = os.Remove(dbFile) }()

	adminAddr := "127.0.0.1:8890" // Different port
	apiAddr := ":8889"

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
			if err != context.Canceled {
				t.Errorf("Server error: %v", err)
			}
		}
	}()

	// Wait for server to start
	waitForServer(t, "http://127.0.0.1:8890/admin/users", 20)

	baseURL := "http://" + adminAddr

	// 1. Get Admin Page (List Users)
	req, _ := http.NewRequest("GET", baseURL+"/", nil)
	req.SetBasicAuth("admin", "1337chat")
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Add User via Form
	form := url.Values{}
	form.Add("username", "ui_testuser")
	req, _ = http.NewRequest("POST", baseURL+"/admin/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "1337chat")

	resp, err = client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// Optionally check body for "User Created!" or link

	// 3. Delete User via Form (redirects)
	form = url.Values{}
	form.Add("username", "ui_testuser")
	req, _ = http.NewRequest("POST", baseURL+"/admin/users/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "1337chat")

	// Client follows redirects by default
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// Verify we are back at root
	require.Equal(t, baseURL+"/", resp.Request.URL.String())
}
