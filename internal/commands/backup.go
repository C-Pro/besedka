package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
)

// Backup triggers an out-of-schedule full backup on the running server without
// stopping it. Requires S3 backup to be enabled server-side.
func Backup(cfg *config.Config) error {
	resp, err := adminRequest(cfg, http.MethodPost, "/api/backup", nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return httpError("trigger backup", resp)
	}

	var result models.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Println("Backup completed.")
	return nil
}
