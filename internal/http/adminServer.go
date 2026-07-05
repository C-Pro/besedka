package http

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/models"
	"besedka/internal/ws"
)

type AdminServer struct {
	server       *http.Server
	wg           sync.WaitGroup
	authService  *auth.AuthService
	hub          *ws.Hub
	tmpl         *template.Template
	adminHandler *api.AdminHandler
	baseURL      string
	chatName     string

	// Server-control ops, injected via SetOps once the API server and backup
	// scheduler exist. Any of them may be nil when the corresponding capability
	// is unavailable (e.g. onBackup is nil when S3 backup is disabled).
	onBackup    func(ctx context.Context) error
	onShutdown  func(ctx context.Context) (backedUp bool, err error)
	triggerExit func(err error)
}

// SetOps injects the server-control callbacks used by the /api/backup and
// /api/shutdown endpoints. onBackup runs a single full backup without stopping
// the server (nil when S3 backup is disabled). onShutdown stops the primary
// server and takes a final backup, returning an error only when that backup
// ultimately fails. triggerExit stops the process; a non-nil error makes the
// process exit non-zero.
func (s *AdminServer) SetOps(
	onBackup func(ctx context.Context) error,
	onShutdown func(ctx context.Context) (bool, error),
	triggerExit func(err error),
) {
	s.onBackup = onBackup
	s.onShutdown = onShutdown
	s.triggerExit = triggerExit
}

func NewAdminServer(cfg *config.Config, authService *auth.AuthService, hub *ws.Hub, assets fs.FS) *AdminServer {
	// Parse admin template
	tmpl, err := template.ParseFS(assets, "admin.html")
	if err != nil {
		slog.Error("failed to parse admin template", "error", err)
		os.Exit(1)
	}

	adminHandler := api.NewAdminHandler(authService, hub, cfg.BaseURL)
	mux := http.NewServeMux()

	// Basic Auth Middleware
	withBasicAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != cfg.AdminUser || pass != cfg.AdminPassword {
				w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	// UI Handlers

	// Wait, to call s.handleListUsers, s must exist.
	// So:
	s := &AdminServer{
		authService:  authService,
		hub:          hub,
		tmpl:         tmpl,
		adminHandler: adminHandler,
		baseURL:      cfg.BaseURL,
		chatName:     cfg.ChatName,
	}

	// UI Handlers
	mux.HandleFunc("GET /", withBasicAuth(s.handleListUsers))
	mux.HandleFunc("POST /admin/users", withBasicAuth(s.handleAddUser))
	mux.HandleFunc("POST /admin/users/delete", withBasicAuth(s.handleDeleteUser))
	mux.HandleFunc("POST /admin/users/reset", withBasicAuth(s.handleResetUser))

	// API Handlers
	mux.HandleFunc("GET /api/users", withBasicAuth(s.handleListUsersJSON))
	mux.HandleFunc("POST /api/users", withBasicAuth(adminHandler.AddUserHandler))
	mux.HandleFunc("DELETE /api/users", withBasicAuth(adminHandler.DeleteUserHandler))
	mux.HandleFunc("POST /api/users/reset-password", withBasicAuth(adminHandler.ResetUserPasswordHandler))

	// Server-control handlers
	mux.HandleFunc("POST /api/backup", withBasicAuth(s.handleBackup))
	mux.HandleFunc("POST /api/shutdown", withBasicAuth(s.handleShutdown))

	addr := cfg.AdminAddr
	if addr == "" {
		addr = "localhost:8081"
	}

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

func (s *AdminServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.authService.GetAllUsers()
	if err != nil {
		http.Error(w, "Failed to get users", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"ChatName": s.chatName,
		"Users":    users,
	}
	if err := s.tmpl.Execute(w, data); err != nil {
		slog.Error("failed to execute template", "error", err)
	}
}

func (s *AdminServer) handleListUsersJSON(w http.ResponseWriter, r *http.Request) {
	users, err := s.authService.GetAllUsers()
	if err != nil {
		http.Error(w, "Failed to get users", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		slog.Error("failed to encode users", "error", err)
	}
}

// writeJSONResp writes an APIResponse with the given status code.
func writeJSONResp(w http.ResponseWriter, status int, resp models.APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// handleBackup triggers a single full backup without stopping the server.
func (s *AdminServer) handleBackup(w http.ResponseWriter, r *http.Request) {
	if s.onBackup == nil {
		writeJSONResp(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "S3 backup not enabled",
		})
		return
	}

	if err := s.onBackup(r.Context()); err != nil {
		slog.Error("on-demand backup failed", "error", err)
		writeJSONResp(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("backup failed: %v", err),
		})
		return
	}

	writeJSONResp(w, http.StatusOK, models.APIResponse{Success: true, Message: "backup completed"})
}

// handleShutdown stops the primary server, takes a final backup, and then stops
// the process. On backup failure it responds 500 and still exits the process
// with a non-zero code so the operator knows the shutdown was not clean.
func (s *AdminServer) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if s.onShutdown == nil || s.triggerExit == nil {
		writeJSONResp(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "shutdown not available",
		})
		return
	}

	backedUp, err := s.onShutdown(r.Context())
	if err != nil {
		writeJSONResp(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("shutdown aborted: %v", err),
		})
		flush(w)
		s.triggerExit(err)
		return
	}

	msg := "primary server stopped; shutting down"
	if backedUp {
		msg = "primary server stopped; final backup complete; shutting down"
	}
	writeJSONResp(w, http.StatusOK, models.APIResponse{Success: true, Message: msg})
	flush(w)
	s.triggerExit(nil)
}

// flush best-effort flushes the response so the CLI receives it before the
// process begins tearing down.
func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *AdminServer) handleAddUser(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		s.adminHandler.AddUserHandler(w, r)
		return
	}

	username := r.FormValue("username")
	if username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}

	token, err := s.authService.AddUser(username, username)

	data := map[string]any{"ChatName": s.chatName}
	if err != nil {
		data["Error"] = err.Error()
	} else {
		base := strings.TrimRight(s.baseURL, "/")
		data["NewLink"] = fmt.Sprintf("%s/register.html?token=%s", base, url.QueryEscape(token))
	}

	// Refresh user list
	users, _ := s.authService.GetAllUsers()
	data["Users"] = users

	if err := s.tmpl.Execute(w, data); err != nil {
		slog.Error("failed to execute template", "error", err)
	}
}

func (s *AdminServer) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.FormValue("id")
	if userID != "" {
		if err := s.authService.DeleteUser(userID); err == nil {
			s.hub.RemoveDeletedUser(userID)
		}
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *AdminServer) handleResetUser(w http.ResponseWriter, r *http.Request) {
	userID := r.FormValue("id")
	data := map[string]any{"ChatName": s.chatName}
	if userID == "" {
		data["Error"] = "User ID is required"
	} else {
		token, err := s.authService.ResetPassword(userID, true)
		if err != nil {
			data["Error"] = "Failed to reset password: " + err.Error()
		} else {
			s.hub.DisconnectUser(userID)
			base := strings.TrimRight(s.baseURL, "/")
			data["NewLink"] = fmt.Sprintf("%s/register.html?token=%s", base, url.QueryEscape(token))
		}
	}

	// Refresh user list
	users, _ := s.authService.GetAllUsers()
	data["Users"] = users

	if err := s.tmpl.Execute(w, data); err != nil {
		slog.Error("failed to execute template", "error", err)
	}
}

func (s *AdminServer) Server() *http.Server {
	return s.server
}

func (s *AdminServer) Start() error {
	slog.Info("Admin API started", "address", s.server.Addr)
	s.wg.Add(1)
	defer s.wg.Done()

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *AdminServer) Shutdown(ctx context.Context) error {
	defer s.wg.Wait()
	return s.server.Shutdown(ctx)
}
