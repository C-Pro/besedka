package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

func DeleteUser(username string, assumeYes bool, cfg *config.Config) error {
	userID, err := resolveUserID(cfg, username)
	if err != nil {
		return err
	}

	if !assumeYes {
		ok, err := confirm(os.Stdin, os.Stdout, fmt.Sprintf("Delete user %s (%s)? [y/N]: ", username, userID))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Aborted.")
			return nil
		}
	}

	resp, err := adminRequest(cfg, http.MethodDelete, "/api/users?id="+url.QueryEscape(userID), nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return httpError("delete user", resp)
	}

	var result models.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("User %s deleted.\n", username)
	return nil
}
