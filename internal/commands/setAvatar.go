package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
)

func SetAvatar(filePath, username string, cfg *config.Config) error {
	if filePath == "" {
		return errors.New("avatar file path is required")
	}
	if username == "" {
		return errors.New("--user is required when using --set-avatar")
	}

	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read avatar file %q: %w", filePath, err)
	}

	userID, err := resolveUserID(cfg, username)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s/api/users/set-avatar?id=%s", cfg.AdminAddr, userID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(fileData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.SetBasicAuth(cfg.AdminUser, cfg.AdminPassword)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call admin API: %w. Is the server running?", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return httpError("set avatar", resp)
	}

	var result models.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("\nAvatar updated successfully for user %s!\n", username)
	return nil
}
