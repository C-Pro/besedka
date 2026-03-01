package commands

import (
	"besedka/internal/api"
	"besedka/internal/config"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func AddUser(username string, cfg *config.Config) error {
	// Prepare request
	reqBody, err := json.Marshal(api.AddUserRequest{Username: username})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/admin/users", cfg.AdminAddr)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Use config for admin credentials
	req.SetBasicAuth(cfg.AdminUser, cfg.AdminPassword)

	client := &http.Client{}
	resp, err := client.Do(req)
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

	fmt.Printf("Setup Link:         %s\n\n", result.SetupLink)
	fmt.Println("Please share this link with the user to complete registration.")
	return nil
}
