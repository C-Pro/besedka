package commands

import (
	"besedka/internal/config"
	"besedka/internal/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testServer spins up an httptest server with the given handler and returns a
// config pointing the CLI commands at it with fixed admin credentials.
func testServer(t *testing.T, handler http.HandlerFunc) (*config.Config, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	cfg := &config.Config{
		AdminAddr:     strings.TrimPrefix(ts.URL, "http://"),
		AdminUser:     "admin",
		AdminPassword: "secret",
	}
	return cfg, ts
}

// requireAuth fails the request unless it carries the expected basic auth.
func requireAuth(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	user, pass, ok := r.BasicAuth()
	if !ok || user != "admin" || pass != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

func writeUsers(w http.ResponseWriter, users []models.User) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(users)
}

func TestResolveUserID(t *testing.T) {
	users := []models.User{
		{ID: "u1", UserName: "alice", Status: models.UserStatusActive},
		{ID: "u2", UserName: "bob", Status: models.UserStatusCreated},
		{ID: "u3", UserName: "ghost", Status: models.UserStatusDeleted},
	}
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, w, r) {
			return
		}
		writeUsers(w, users)
	})

	t.Run("active match", func(t *testing.T) {
		id, err := resolveUserID(cfg, "alice")
		if err != nil || id != "u1" {
			t.Fatalf("got id=%q err=%v, want u1/nil", id, err)
		}
	})

	t.Run("created match", func(t *testing.T) {
		id, err := resolveUserID(cfg, "bob")
		if err != nil || id != "u2" {
			t.Fatalf("got id=%q err=%v, want u2/nil", id, err)
		}
	})

	t.Run("deleted is ignored", func(t *testing.T) {
		if _, err := resolveUserID(cfg, "ghost"); err == nil {
			t.Fatal("expected error for deleted-only username")
		}
	})

	t.Run("not found", func(t *testing.T) {
		if _, err := resolveUserID(cfg, "nobody"); err == nil {
			t.Fatal("expected error for unknown username")
		}
	})
}

func TestResolveUserIDAmbiguous(t *testing.T) {
	users := []models.User{
		{ID: "u1", UserName: "dup", Status: models.UserStatusActive},
		{ID: "u2", UserName: "dup", Status: models.UserStatusCreated},
	}
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeUsers(w, users)
	})
	if _, err := resolveUserID(cfg, "dup"); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestListUsers(t *testing.T) {
	var gotMethod, gotPath string
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, w, r) {
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		writeUsers(w, []models.User{{ID: "u1", UserName: "alice", Status: models.UserStatusActive}})
	})
	if err := ListUsers(cfg); err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/users" {
		t.Fatalf("got %s %s, want GET /api/users", gotMethod, gotPath)
	}
}

func TestListUsersUnauthorized(t *testing.T) {
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if err := ListUsers(cfg); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestDeleteUser(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, w, r) {
			return
		}
		// resolveUserID does GET /api/users; the delete is DELETE /api/users.
		if r.Method == http.MethodGet && r.URL.Path == "/api/users" {
			writeUsers(w, []models.User{{ID: "u1", UserName: "alice", Status: models.UserStatusActive}})
			return
		}
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(models.APIResponse{Success: true, Message: "deleted"})
	})

	if err := DeleteUser("alice", true, cfg); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/users" || gotQuery != "id=u1" {
		t.Fatalf("got %s %s?%s, want DELETE /api/users?id=u1", gotMethod, gotPath, gotQuery)
	}
}

func TestDeleteUserNotFound(t *testing.T) {
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeUsers(w, []models.User{}) // resolveUserID finds nothing
	})
	if err := DeleteUser("ghost", true, cfg); err == nil {
		t.Fatal("expected error when user cannot be resolved")
	}
}

func TestResetPassword(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, w, r) {
			return
		}
		if r.URL.Path == "/api/users" {
			writeUsers(w, []models.User{{ID: "u9", UserName: "alice", Status: models.UserStatusActive}})
			return
		}
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(models.ResetPasswordResponse{
			APIResponse: models.APIResponse{Success: true},
			SetupLink:   "http://x/register.html?token=abc",
		})
	})
	if err := ResetPassword("alice", cfg); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/users/reset-password" || gotQuery != "id=u9" {
		t.Fatalf("got %s %s?%s, want POST /api/users/reset-password?id=u9", gotMethod, gotPath, gotQuery)
	}
}

func TestBackup(t *testing.T) {
	var gotMethod, gotPath string
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, w, r) {
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewEncoder(w).Encode(models.APIResponse{Success: true, Message: "backup completed"})
	})
	if err := Backup(cfg); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/backup" {
		t.Fatalf("got %s %s, want POST /api/backup", gotMethod, gotPath)
	}
}

func TestBackupServerError(t *testing.T) {
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(models.APIResponse{Success: false, Message: "S3 backup not enabled"})
	})
	err := Backup(cfg)
	if err == nil || !strings.Contains(err.Error(), "S3 backup not enabled") {
		t.Fatalf("expected surfaced server message, got %v", err)
	}
}

func TestShutdown(t *testing.T) {
	var gotMethod, gotPath string
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, w, r) {
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewEncoder(w).Encode(models.APIResponse{Success: true, Message: "shutting down"})
	})
	if err := Shutdown(cfg); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/shutdown" {
		t.Fatalf("got %s %s, want POST /api/shutdown", gotMethod, gotPath)
	}
}

func TestShutdownBackupFailureSurfaced(t *testing.T) {
	cfg, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{Success: false, Message: "shutdown aborted: backup failed"})
	})
	err := Shutdown(cfg)
	if err == nil || !strings.Contains(err.Error(), "backup failed") {
		t.Fatalf("expected surfaced backup failure, got %v", err)
	}
}

func TestConfirm(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"\n", false},
		{"nope\n", false},
		{"", false}, // EOF
	}
	for _, c := range cases {
		got, err := confirm(strings.NewReader(c.in), &strings.Builder{}, "prompt")
		if err != nil {
			t.Fatalf("confirm(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("confirm(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPrintUsers(t *testing.T) {
	var b strings.Builder
	printUsers(&b, []models.User{
		{ID: "u1", UserName: "alice", DisplayName: "Alice", Status: models.UserStatusActive, Presence: models.Presence{Online: true}},
	})
	out := b.String()
	for _, want := range []string{"USERNAME", "alice", "Alice", "active", "yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	var empty strings.Builder
	printUsers(&empty, nil)
	if !strings.Contains(empty.String(), "(no users)") {
		t.Errorf("empty output missing placeholder:\n%s", empty.String())
	}
}
