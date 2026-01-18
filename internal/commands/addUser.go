package commands

import (
	"besedka/internal/api"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func AddUser(username string) error {
	adminAddr := os.Getenv("ADMIN_ADDR")
	if adminAddr == "" {
		adminAddr = "localhost:8081"
	}
	// Prepare request
	reqBody, err := json.Marshal(api.AddUserRequest{Username: username})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/admin/users", adminAddr)
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
	fmt.Printf("Temporary Password: %s\n", result.Password)
	apiAddr := os.Getenv("API_ADDR")
	if apiAddr == "" {
		apiAddr = ":8080"
	}
	// If apiAddr starts with :, preprend localhost
	if len(apiAddr) > 0 && apiAddr[0] == ':' {
		apiAddr = "localhost" + apiAddr
	}

	fmt.Printf("Setup Link:         http://%s%s\n\n", apiAddr, result.SetupLink)
	fmt.Println("Please share these details with the user securely.")
	return nil
}
