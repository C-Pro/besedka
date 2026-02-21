package http

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/ws"
	"besedka/static"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type AdminServer struct {
	server       *http.Server
	wg           sync.WaitGroup
	authService  *auth.AuthService
	hub          *ws.Hub
	tmpl         *template.Template
	adminHandler *api.AdminHandler
}

func NewAdminServer(cfg *config.Config, authService *auth.AuthService, hub *ws.Hub) *AdminServer {
	// Parse admin template
	tmpl, err := template.ParseFS(static.Content, "admin.html")
	if err != nil {
		log.Fatalf("failed to parse admin template: %v", err)
	}

	adminHandler := api.NewAdminHandler(authService, hub)
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
	}

	// UI Handlers
	mux.HandleFunc("GET /", withBasicAuth(s.handleListUsers))
	mux.HandleFunc("POST /admin/users", withBasicAuth(s.handleAddUser))
	mux.HandleFunc("POST /admin/users/delete", withBasicAuth(s.handleDeleteUser))

	// API Handlers
	mux.HandleFunc("GET /api/users", withBasicAuth(s.handleListUsersJSON))
	mux.HandleFunc("POST /api/users", withBasicAuth(adminHandler.AddUserHandler))
	mux.HandleFunc("DELETE /api/users", withBasicAuth(adminHandler.DeleteUserHandler))
	mux.HandleFunc("DELETE /admin/users", withBasicAuth(adminHandler.DeleteUserHandler))

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
	data := map[string]interface{}{
		"Users": users,
	}
	if err := s.tmpl.Execute(w, data); err != nil {
		log.Printf("failed to execute template: %v", err)
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
		log.Printf("failed to encode users: %v", err)
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

	data := map[string]interface{}{}
	if err != nil {
		data["Error"] = err.Error()
	} else {
		data["NewLink"] = fmt.Sprintf("/register.html?token=%s", url.QueryEscape(token))
	}

	// Refresh user list
	users, _ := s.authService.GetAllUsers()
	data["Users"] = users

	if err := s.tmpl.Execute(w, data); err != nil {
		log.Printf("failed to execute template: %v", err)
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

func (s *AdminServer) Server() *http.Server {
	return s.server
}

func (s *AdminServer) Start() error {
	log.Printf("Admin API started on %s", s.server.Addr)
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
