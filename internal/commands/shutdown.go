package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
)

// Shutdown asks the running server to stop its primary HTTP server, take a
// final full backup (when S3 is enabled), and then exit. The call blocks until
// the backup completes so a successful return guarantees the final state was
// captured. A backup failure surfaces here as a non-nil error.
func Shutdown(cfg *config.Config) error {
	resp, err := adminRequest(cfg, http.MethodPost, "/api/shutdown", nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return httpError("shut down server", resp)
	}

	var result models.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Message != "" {
		fmt.Println(result.Message)
	} else {
		fmt.Println("Server shutting down.")
	}
	return nil
}
