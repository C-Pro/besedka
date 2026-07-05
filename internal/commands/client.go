// Package commands implements the CLI subcommands that drive the running
// server's Basic-Auth-protected admin API (add/list/delete/reset users, trigger
// an out-of-schedule backup, and graceful shutdown). Every command talks to the
// server over HTTP at cfg.AdminAddr, so the server must be running.
package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// httpClient is used for all admin API calls. It has no timeout on purpose:
// backup and shutdown can block while a snapshot is uploaded to object storage.
var httpClient = &http.Client{}

// adminRequest builds and sends an authenticated request to the running
// server's admin API. When body is non-nil it is JSON-encoded and the
// Content-Type header is set. The caller owns resp.Body and must close it.
func adminRequest(cfg *config.Config, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	url := fmt.Sprintf("http://%s%s", cfg.AdminAddr, path)
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(cfg.AdminUser, cfg.AdminPassword)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call admin API: %w. Is the server running?", err)
	}
	return resp, nil
}

// httpError reads the response body and returns a formatted error for a
// non-success status code. When the body is a JSON APIResponse its Message is
// surfaced; otherwise the raw body is used.
func httpError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(body))
	var apiResp models.APIResponse
	if json.Unmarshal(body, &apiResp) == nil && apiResp.Message != "" {
		msg = apiResp.Message
	}
	return fmt.Errorf("failed to %s (status %d): %s", action, resp.StatusCode, msg)
}

// resolveUserID looks up the ID of the single non-deleted user with the given
// username via GET /api/users. It errors when there is no match or the name is
// ambiguous, so a typo never silently targets the wrong user.
func resolveUserID(cfg *config.Config, username string) (string, error) {
	resp, err := adminRequest(cfg, http.MethodGet, "/api/users", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", httpError("list users", resp)
	}

	var users []models.User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", fmt.Errorf("failed to decode users: %w", err)
	}

	var matches []models.User
	for _, u := range users {
		if u.UserName == username && u.Status != models.UserStatusDeleted {
			matches = append(matches, u)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no active user found with username %q", username)
	case 1:
		return matches[0].ID, nil
	default:
		return "", fmt.Errorf("multiple users found with username %q; delete/reset by ID is not supported", username)
	}
}

// confirm prints prompt to w and reads a yes/no answer from r. Only "y" or
// "yes" (case-insensitive) count as confirmation; anything else, including an
// empty line, is treated as no.
func confirm(r io.Reader, w io.Writer, prompt string) (bool, error) {
	_, _ = fmt.Fprint(w, prompt)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
