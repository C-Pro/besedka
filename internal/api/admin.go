package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"besedka/internal/audio"
	"besedka/internal/auth"
	"besedka/internal/images"
	"besedka/internal/models"
	"besedka/internal/storage"
	"besedka/internal/ws"

	"github.com/google/uuid"
	"github.com/h2non/filetype"
)

type AdminHandler struct {
	authService   *auth.AuthService
	hub           *ws.Hub
	storage       *storage.BboltStorage
	baseURL       string
	maxAvatarSize int64
}

func NewAdminHandler(authService *auth.AuthService, hub *ws.Hub, store *storage.BboltStorage, baseURL string, maxAvatarSize int64) *AdminHandler {
	return &AdminHandler{
		authService:   authService,
		hub:           hub,
		storage:       store,
		baseURL:       baseURL,
		maxAvatarSize: maxAvatarSize,
	}
}

type AddUserRequest struct {
	Username       string                `json:"username"`
	DisplayName    string                `json:"displayName,omitempty"`
	Type           string                `json:"type,omitempty"`
	BotPermissions models.BotPermissions `json:"botPermissions,omitempty"`
	Target         string                `json:"target,omitempty"`
	TargetChat     string                `json:"targetChat,omitempty"`
}

type AddUserResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Username  string `json:"username,omitempty"`
	SetupLink string `json:"setupLink,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
}

func (h *AdminHandler) addBotHandler(w http.ResponseWriter, req AddUserRequest, displayName string) {
	user, apiKey, err := h.authService.AddBot(req.Username, displayName, req.BotPermissions)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(AddUserResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create bot: %v", err),
		})
		return
	}
	if allUsers, err := h.authService.GetUsers(); err == nil {
		h.hub.EnsureDMsFor(user, allUsers)
		go h.hub.BroadcastNewUser(user)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AddUserResponse{
		Success:  true,
		Username: user.UserName,
		APIKey:   apiKey,
	})
}

func (h *AdminHandler) addWebhookHandler(w http.ResponseWriter, req AddUserRequest, displayName string) {
	target := req.Target
	if target == "" {
		target = req.TargetChat
	}
	if target == "" {
		target = "townhall"
	}
	user, apiKey, err := h.authService.AddWebhook(req.Username, displayName, target)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(AddUserResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create webhook: %v", err),
		})
		return
	}
	if allUsers, err := h.authService.GetUsers(); err == nil {
		h.hub.EnsureDMsFor(user, allUsers)
		go h.hub.BroadcastNewUser(user)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AddUserResponse{
		Success:  true,
		Username: user.UserName,
		APIKey:   apiKey,
	})
}

func (h *AdminHandler) AddUserHandler(w http.ResponseWriter, r *http.Request) {
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

	if req.Type == string(models.UserTypeBot) {
		h.addBotHandler(w, req, displayName)
		return
	}

	if req.Type == string(models.UserTypeWebhook) {
		h.addWebhookHandler(w, req, displayName)
		return
	}

	token, err := h.authService.AddUser(req.Username, displayName)
	if err != nil {
		resp := AddUserResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create user: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Create DMs for the new user
	allUsers, err := h.authService.GetUsers()
	if err == nil {
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
	userID := r.URL.Query().Get("id")
	if userID == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	token, err := h.authService.ResetPassword(userID, true)
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

func (h *AdminHandler) ResetAPIKeyHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	if userID == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	apiKey, err := h.authService.ResetAPIKey(userID)
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
			Message: fmt.Sprintf("Failed to reset API key: %v", err),
		})
		return
	}

	h.hub.DisconnectUser(userID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AddUserResponse{
		Success: true,
		Message: fmt.Sprintf("API key for user %s reset successfully", userID),
		APIKey:  apiKey,
	})
}

func (h *AdminHandler) SetUserAvatarHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	if userID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "User ID is required",
		})
		return
	}

	allUsers, err := h.authService.GetAllUsers()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Failed to query users",
		})
		return
	}

	var found bool
	for _, u := range allUsers {
		if u.ID == userID && u.Status != models.UserStatusDeleted {
			found = true
			break
		}
	}
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "User not found",
		})
		return
	}

	maxBytes := h.maxAvatarSize
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Failed to read request body",
		})
		return
	}
	data := buf.Bytes()

	if len(data) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Avatar file is empty",
		})
		return
	}

	if !filetype.IsImage(data) && !isSVG(data) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Invalid file type. Only images are allowed.",
		})
		return
	}

	mimeType := "application/octet-stream"
	if kind, err := filetype.Match(data); err == nil && kind != filetype.Unknown {
		mimeType = audio.NormalizeMimeType(kind.MIME.Value)
	} else if isSVG(data) {
		mimeType = "image/svg+xml"
	}

	hasher := sha256.New()
	hasher.Write(data)
	hash := hex.EncodeToString(hasher.Sum(nil))

	if h.storage == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Storage not initialized",
		})
		return
	}

	if err := h.storage.SaveFileBlob(bytes.NewReader(data), hash); err != nil {
		slog.Error("failed to save avatar file blob", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Internal Storage Error",
		})
		return
	}

	fileID := uuid.NewString()
	meta := storage.FileMetadata{
		ID:        fileID,
		Hash:      hash,
		MimeType:  mimeType,
		Size:      int64(len(data)),
		CreatedAt: time.Now().Unix(),
		UserID:    userID,
		ChatID:    "",
	}

	if _, err := images.AttachThumbnail(h.storage, &meta, data); err != nil {
		slog.Warn("thumbnail generation failed for admin set avatar", "fileID", fileID, "error", err)
	}

	if err := h.storage.UpsertFileMetadata(meta); err != nil {
		slog.Error("failed to save avatar file metadata", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Internal Database Error",
		})
		return
	}

	avatarURL := fmt.Sprintf("/api/images/%s?thumb=1", fileID)
	if err := h.authService.UpdateAvatarURL(userID, avatarURL); err != nil {
		slog.Error("failed to update user avatar url", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Failed to update user avatar URL",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Avatar updated successfully",
	})
}

