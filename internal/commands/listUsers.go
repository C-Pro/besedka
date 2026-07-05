package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
)

func ListUsers(cfg *config.Config) error {
	resp, err := adminRequest(cfg, http.MethodGet, "/api/users", nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return httpError("list users", resp)
	}

	var users []models.User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	printUsers(os.Stdout, users)
	return nil
}

// printUsers renders users as an aligned table.
func printUsers(w io.Writer, users []models.User) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tUSERNAME\tDISPLAY NAME\tSTATUS\tONLINE")
	for _, u := range users {
		online := "no"
		if u.Presence.Online {
			online = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.ID, u.UserName, u.DisplayName, u.Status, online)
	}
	_ = tw.Flush()
	if len(users) == 0 {
		_, _ = fmt.Fprintln(w, "(no users)")
	}
}
