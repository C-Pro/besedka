package api

import (
	"besedka/internal/auth"
	"besedka/internal/models"
	"besedka/internal/ws"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type AdminHandler struct {
	authService *auth.AuthService
	hub         *ws.Hub
}

func NewAdminHandler(authService *auth.AuthService, hub *ws.Hub) *AdminHandler {
	return &AdminHandler{authService: authService, hub: hub}
}

type AddUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
}

type AddUserResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Username  string `json:"username,omitempty"`
	SetupLink string `json:"setupLink,omitempty"`
}

func (h *AdminHandler) AddUserHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("DEBUG: AddUserHandler called %s %s\n", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AddUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Username
	}

	token, err := h.authService.AddUser(req.Username, displayName)
	if err != nil {
		resp := AddUserResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create user: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			// Best effort logging
			_ = err
		}
		return
	}

	// Create DMs for the new user
	allUsers, err := h.authService.GetUsers()
	if err == nil {
		// Find the new user
		var newUser models.User
		for _, u := range allUsers {
			if u.UserName == req.Username {
				newUser = u
				break
			}
		}
		if newUser.ID != "" {
			h.hub.EnsureDMsFor(newUser, allUsers)
		}
	}

	resp := AddUserResponse{
		Success:   true,
		Username:  req.Username,
		SetupLink: fmt.Sprintf("/register.html?token=%s", url.QueryEscape(token)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		_ = err
	}
}
