package api

import (
	"besedka/internal/auth"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

type AdminHandler struct {
	authService *auth.AuthService
}

func NewAdminHandler(authService *auth.AuthService) *AdminHandler {
	return &AdminHandler{authService: authService}
}

type AddUserRequest struct {
	Username string `json:"username"`
}

type AddUserResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
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

	password, err := generateRandomPassword(12)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	user, err := h.authService.AddUser(req.Username, password)
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

	resp := AddUserResponse{
		Success:   true,
		Username:  user.UserName,
		Password:  password,
		SetupLink: fmt.Sprintf("/register.html?username=%s", user.UserName),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		_ = err
	}
}

func generateRandomPassword(length int) (string, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b)[:length], nil
}
