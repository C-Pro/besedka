package commands

import (
	"besedka/internal/api"
	"besedka/internal/config"
	"besedka/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func AddUser(username, displayName, userType, botPermissionsStr, target string, cfg *config.Config) error {
	perms := models.BotPermissions{}
	if botPermissionsStr != "" {
		for _, p := range strings.Split(botPermissionsStr, ",") {
			p = strings.TrimSpace(p)
			switch p {
			case "read_mentions":
				perms.ReadMentions = true
			case "read_all":
				perms.ReadAll = true
			case "write":
				perms.Write = true
			}
		}
	}

	req := api.AddUserRequest{
		Username:       username,
		DisplayName:    displayName,
		Type:           userType,
		BotPermissions: perms,
		Target:         target,
	}

	resp, err := adminRequest(cfg, http.MethodPost, "/admin/users", req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return httpError("add user", resp)
	}

	var result api.AddUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("\nUser Created Successfully!\n")
	fmt.Printf("Username:          %s\n", result.Username)
	if result.APIKey != "" {
		fmt.Printf("API Key:           %s\n\n", result.APIKey)
		fmt.Println("Please store this API Key safely. It will not be shown again.")
	} else {
		fmt.Printf("Setup Link:         %s\n\n", result.SetupLink)
		fmt.Println("Please share this link with the user to complete registration.")
	}
	return nil
}
