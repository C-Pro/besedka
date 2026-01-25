package commands

import (
	"besedka/internal/api"
	"besedka/internal/config"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func AddUser(username string, cfg *config.Config) error {
	// Prepare request
	reqBody, err := json.Marshal(api.AddUserRequest{Username: username})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/admin/users", cfg.AdminAddr)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to call admin API: %w. Is the server running?", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add user (Status: %d): %s", resp.StatusCode, string(body))
	}

	var result api.AddUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("\nUser Created Successfully!\n")
	fmt.Printf("Username:          %s\n", result.Username)

	baseURL := cfg.BaseURL
	// Ensure BaseURL doesn't end with slash if SetupLink starts with one, or handle neatly.
	// SetupLink from API usually starts with /register.html?token=...
	// Let's trim suffix from baseURL just in case
	baseURL = strings.TrimSuffix(baseURL, "/")

	setupLink := result.SetupLink
	if !strings.HasPrefix(setupLink, "/") {
		setupLink = "/" + setupLink
	}

	fmt.Printf("Setup Link:         %s%s\n\n", baseURL, setupLink)
	fmt.Println("Please share this link with the user to complete registration.")
	return nil
}
