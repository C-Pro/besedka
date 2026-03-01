package api

import (
	"besedka/internal/auth"
	"besedka/internal/models"
	"besedka/internal/ws"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type AdminHandler struct {
	authService *auth.AuthService
	hub         *ws.Hub
	baseURL     string
}

func NewAdminHandler(authService *auth.AuthService, hub *ws.Hub, baseURL string) *AdminHandler {
	return &AdminHandler{authService: authService, hub: hub, baseURL: baseURL}
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

	base := strings.TrimRight(h.baseURL, "/")
	resp := AddUserResponse{
		Success:   true,
		Username:  req.Username,
		SetupLink: fmt.Sprintf("%s/register.html?token=%s", base, url.QueryEscape(token)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		_ = err
	}
}

func (h *AdminHandler) DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	if userID == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	if err := h.authService.DeleteUser(userID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			if err := json.NewEncoder(w).Encode(models.APIResponse{
				Success: false,
				Message: "User not found",
			}); err != nil {
				_ = err
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to delete user: %v", err),
		}); err != nil {
			_ = err
		}
		return
	}

	h.hub.RemoveDeletedUser(userID)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: fmt.Sprintf("User %s deleted", userID),
	}); err != nil {
		_ = err
	}
}

func (h *AdminHandler) ResetUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("id")
	if userID == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	token, err := h.authService.ResetPassword(userID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(models.APIResponse{
				Success: false,
				Message: "User not found",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to reset user password: %v", err),
		})
		return
	}

	h.hub.DisconnectUser(userID)

	base := strings.TrimRight(h.baseURL, "/")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.ResetPasswordResponse{
		APIResponse: models.APIResponse{
			Success: true,
			Message: fmt.Sprintf("Password for user %s reset successfully", userID),
		},
		SetupLink: fmt.Sprintf("%s/register.html?token=%s", base, url.QueryEscape(token)),
	})
}
