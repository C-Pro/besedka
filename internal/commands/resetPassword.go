package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

func ResetPassword(username string, cfg *config.Config) error {
	userID, err := resolveUserID(cfg, username)
	if err != nil {
		return err
	}

	resp, err := adminRequest(cfg, http.MethodPost, "/api/users/reset-password?id="+url.QueryEscape(userID), nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return httpError("reset password", resp)
	}

	var result models.ResetPasswordResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("\nPassword reset for user %s.\n", username)
	fmt.Printf("Setup Link:         %s\n\n", result.SetupLink)
	fmt.Println("Please share this link with the user to complete registration.")
	return nil
}
