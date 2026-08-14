package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"besedka/internal/audio"
	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/content"
	"besedka/internal/images"
	"besedka/internal/models"
	"besedka/internal/storage"
	"besedka/internal/ws"

	"github.com/google/uuid"
	"github.com/h2non/filetype"
)

type PushService interface {
	PublicKey() string
	SendNotification(userID string, payload []byte) error
}

type API struct {
	auth    *auth.AuthService
	hub     *ws.Hub
	storage *storage.BboltStorage
	cfg     *config.Config
	push    PushService
}

func New(auth *auth.AuthService, hub *ws.Hub, storage *storage.BboltStorage, cfg *config.Config, push PushService) *API {
	return &API{
		auth:    auth,
		hub:     hub,
		storage: storage,
		cfg:     cfg,
		push:    push,
	}
}

func (a *API) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     int    `json:"totp"`
	}

	// Support both JSON and Form (since frontend uses x-www-form-urlencoded)
	if r.Header.Get("Content-Type") == "application/json" {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
		if t := r.FormValue("totp"); t != "" {
			_, _ = fmt.Sscanf(t, "%d", &req.TOTP)
		}
	}

	if err := content.ValidateUsername(req.Username); err != nil {
		http.Error(w, "Invalid username: "+err.Error(), http.StatusBadRequest)
		return
	}

	loginResp, _ := a.auth.Login(auth.LoginRequest{
		Username: req.Username,
		Password: req.Password,
		TOTP:     req.TOTP,
	})

	if !loginResp.Success {
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(loginResp); err != nil {
			slog.Error("failed to encode login response", "error", err)
		}
		return
	}

	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    loginResp.Token,
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.auth.RPOrigin, "https://"),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		Expires:  time.Unix(loginResp.TokenExpiry, 0),
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(loginResp); err != nil {
		slog.Error("failed to encode login response", "error", err)
	}
}

func (a *API) getToken(r *http.Request) string {
	token := r.Header.Get("token")
	if token == "" {
		if c, err := r.Cookie("token"); err == nil {
			token = c.Value
		}
	}
	return token
}

type contextKey string

const (
	userKey       = contextKey("user")
	apiKeyAuthKey = contextKey("apiKeyAuth")
)

// UserFromContext returns the authenticated models.User from context.
func UserFromContext(ctx context.Context) (models.User, bool) {
	u, ok := ctx.Value(userKey).(models.User)
	return u, ok
}

// RequireUserTypes restricts endpoint access to specified UserTypes.
func RequireUserTypes(next http.HandlerFunc, allowedTypes ...models.UserType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		for _, t := range allowedTypes {
			if user.Type == t {
				next(w, r)
				return
			}
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
	}
}

func (a *API) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			if apiKey != "" {
				user, err := a.auth.GetUserByAPIKey(apiKey)
				if err == nil {
					if user.Type == "" {
						user.Type = models.UserTypeHuman
					}
					ctx := context.WithValue(r.Context(), userKey, user)
					ctx = context.WithValue(ctx, apiKeyAuthKey, true)
					next(w, r.WithContext(ctx))
					return
				}
			}
		}

		token := a.getToken(r)
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID, expiry, err := a.auth.GetUserID(token)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := a.auth.GetUser(userID)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if user.Type == "" {
			user.Type = models.UserTypeHuman
		}

		if cookie, err := r.Cookie("token"); err == nil && cookie.Value == token && !expiry.IsZero() {
			http.SetCookie(w, &http.Cookie{
				Name:     "token",
				Value:    token,
				HttpOnly: true,
				Secure:   true,
				Path:     "/",
				Expires:  expiry,
			})
		}

		ctx := context.WithValue(r.Context(), userKey, user)
		next(w, r.WithContext(ctx))
	}
}

func (a *API) LogoffHandler(w http.ResponseWriter, r *http.Request) {
	token := a.getToken(r)
	if token != "" {
		_ = a.auth.Logoff(token)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusOK)
}

func (a *API) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req auth.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Sanitize inputs
	req.DisplayName = content.Sanitize(req.DisplayName)

	resp, _ := a.auth.CompleteRegistration(req)
	if !resp.Success {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("failed to encode register response", "error", err)
		}
		return
	}

	// Update Hub with new user DMs and broadcast
	if user, err := a.auth.GetUser(resp.UserID); err == nil {
		if users, err := a.auth.GetUsers(); err == nil {
			a.hub.EnsureDMsFor(user, users)
		}
		go a.hub.BroadcastNewUser(user)
	}

	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    resp.Token,
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.auth.RPOrigin, "https://"),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		Expires:  time.Now().Add(a.auth.TokenExpiry),
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode register response", "error", err)
	}
}

func (a *API) RegisterInfoHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	info, err := a.auth.GetRegistrationInfo(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Escape output
	info.DisplayName = content.Escape(info.DisplayName)
	info.Username = content.Escape(info.Username)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		slog.Error("failed to encode register info response", "error", err)
	}
}

func (a *API) UsersHandler(w http.ResponseWriter, r *http.Request) {
	users, err := a.auth.GetUsers()
	if err != nil {
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}

	// Escape output and update online status
	for i := range users {
		users[i].DisplayName = content.Escape(users[i].DisplayName)
		users[i].UserName = content.Escape(users[i].UserName)
		users[i].Bio = content.Escape(users[i].Bio)
		users[i].Presence.Online = a.hub.IsUserOnline(users[i].ID)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		slog.Error("failed to encode users response", "error", err)
	}
}

func (a *API) ChatsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	chats := a.hub.GetChats(user.ID)

	// Escape output
	for i := range chats {
		chats[i].Name = content.Escape(chats[i].Name)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(chats); err != nil {
		slog.Error("failed to encode chats response", "error", err)
	}
}

func (a *API) ChatMessagesHandler(w http.ResponseWriter, r *http.Request) {
	chatID := r.PathValue("id")
	if chatID == "" {
		http.Error(w, "Missing chat ID", http.StatusBadRequest)
		return
	}

	fromSeqStr := r.URL.Query().Get("fromSeq")
	toSeqStr := r.URL.Query().Get("toSeq")

	var fromSeq, toSeq int64
	var err error

	if fromSeqStr != "" {
		fromSeq, err = strconv.ParseInt(fromSeqStr, 10, 64)
		if err != nil || fromSeq < 1 {
			fromSeq = 1
		}
	} else {
		fromSeq = 1
	}

	if toSeqStr != "" {
		toSeq, err = strconv.ParseInt(toSeqStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid toSeq", http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, "Missing toSeq", http.StatusBadRequest)
		return
	}

	if fromSeq > toSeq {
		http.Error(w, "Invalid sequence range", http.StatusBadRequest)
		return
	}

	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch user.Type {
	case models.UserTypeWebhook:
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	case models.UserTypeBot:
		if chatID == "townhall" && !user.BotPermissions.ReadAll && !user.BotPermissions.ReadMentions {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	messages, err := a.hub.GetChatRecords(user.ID, chatID, fromSeq, toSeq)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			http.Error(w, "Chat not found or access denied", http.StatusForbidden)
		} else {
			slog.Error("failed to get chat records", "error", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
		}
		return
	}

	filtered := make([]models.Message, 0, len(messages))
	for _, m := range messages {
		mentions := content.ExtractMentions(m.Content)
		if models.IsMessageVisible(chatID, mentions, user) {
			filtered = append(filtered, m)
		}
	}
	messages = filtered

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(messages); err != nil {
		slog.Error("failed to encode messages response", "error", err)
	}
}

func (a *API) SendMessageHandler(w http.ResponseWriter, r *http.Request) {
	chatID := r.PathValue("id")
	if chatID == "" {
		http.Error(w, "Missing chat ID", http.StatusBadRequest)
		return
	}

	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if user.Type == models.UserTypeWebhook {
		http.Error(w, "Webhooks must post via /api/webhook", http.StatusForbidden)
		return
	}
	if user.Type == models.UserTypeBot && chatID == "townhall" {
		if !user.BotPermissions.Write {
			http.Error(w, "Bot has no write permission in Townhall", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(w, "Message content cannot be empty", http.StatusBadRequest)
		return
	}

	a.hub.Dispatch(user.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  chatID,
		Content: req.Content,
	}, nil)

	w.WriteHeader(http.StatusOK)
}

func (a *API) MeHandler(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := a.getToken(r)
	var tokenExpiry int64
	if token != "" {
		if expiry, err := a.auth.GetTokenExpiry(token); err == nil {
			tokenExpiry = expiry.Unix()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	// Return a simplified structure or the full user struct.
	// The frontend expects { id: ... } at minimum based on existing logic,
	// but having name is good too.
	resp := struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		TokenExpiry  int64  `json:"tokenExpiry,omitempty"`
		SessionLimit int64  `json:"sessionLimit,omitempty"`
	}{
		ID:           currentUser.ID,
		Name:         content.Escape(currentUser.DisplayName),
		TokenExpiry:  tokenExpiry,
		SessionLimit: int64(a.auth.TokenExpiry.Seconds()),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode me response", "error", err)
	}
}

// GetUserSettingsHandler returns the current user's persisted preferences,
// falling back to defaults when none have been saved.
func (a *API) GetUserSettingsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	settings, err := a.auth.GetUserSettings(user.ID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to fetch user settings", "userID", user.ID, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		slog.Error("failed to encode settings response", "error", err)
	}
}

// UpdateUserSettingsHandler persists the current user's preferences. The body
// is decoded into the strongly-typed UserSettings struct.
func (a *API) UpdateUserSettingsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var settings models.UserSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, "Invalid settings payload", http.StatusBadRequest)
		return
	}

	if err := a.auth.UpdateUserSettings(user.ID, settings); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to update user settings", "userID", user.ID, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		slog.Error("failed to encode settings response", "error", err)
	}
}

// validateCSRFSameOrigin implements a simple same-origin check using the Origin
// and Referer headers. It ensures that the request originates from the same host
// as the server, mitigating CSRF attacks for cookie-authenticated endpoints.
func validateCSRFSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		if u.Host != r.Host {
			return false
		}
		return true
	}

	referer := r.Header.Get("Referer")
	if referer == "" {
		return false
	}

	u, err := url.Parse(referer)
	if err != nil {
		return false
	}
	if u.Host != r.Host {
		return false
	}

	return true
}

// RequireSameOrigin is a middleware that enforces same-origin policy for POST requests.
// Requests authenticated via API key (flagged in context by RequireAuth) skip CSRF.
func RequireSameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if _, ok := r.Context().Value(apiKeyAuthKey).(bool); ok {
				next(w, r)
				return
			}
			if !validateCSRFSameOrigin(r) {
				http.Error(w, "Invalid Origin", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func (a *API) ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	regToken, err := a.auth.ResetPassword(user.ID, false)
	if err != nil {
		slog.Error("failed to reset password for user", "userID", user.ID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Failed to reset password",
		})
		return
	}

	// Make sure the resetting user gets logged out from all other active sessions and websockets immediately
	a.hub.DisconnectUser(user.ID) // This disconnects all ws connections

	// Also clear token cookie to log them off this session so they can login via registration link
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.ResetPasswordResponse{
		APIResponse: models.APIResponse{
			Success: true,
		},
		SetupLink: fmt.Sprintf("/register.html?token=%s", url.QueryEscape(regToken)),
	})
}

func isSVG(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return bytes.Contains(trimmed, []byte("<svg"))
	}
	return bytes.HasPrefix(trimmed, []byte("<svg"))
}

func (a *API) processUpload(w http.ResponseWriter, r *http.Request, maxBytes int64, enforceImage bool) (string, error) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", errors.New("unauthorized")
	}
	uploaderID := user.ID

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return "", err
	}
	data := buf.Bytes()

	if enforceImage {
		if !filetype.IsImage(data) && !isSVG(data) {
			http.Error(w, "Invalid file type. Only images are allowed.", http.StatusBadRequest)
			return "", errors.New("invalid file type")
		}
	}

	mimeType := "application/octet-stream"
	if detected := audio.DetectAudioMimeType(data); detected != "" {
		mimeType = detected
	} else if kind, err := filetype.Match(data); err == nil && kind != filetype.Unknown {
		mimeType = audio.NormalizeMimeType(kind.MIME.Value)
	} else if isSVG(data) {
		mimeType = "image/svg+xml"
	}

	hasher := sha256.New()
	hasher.Write(data)
	hash := hex.EncodeToString(hasher.Sum(nil))

	if err := a.storage.SaveFileBlob(bytes.NewReader(data), hash); err != nil {
		slog.Error("failed to save file blob", "error", err)
		http.Error(w, "Internal Storage Error", http.StatusInternalServerError)
		return "", err
	}

	fileID := uuid.NewString()
	meta := storage.FileMetadata{
		ID:        fileID,
		Hash:      hash,
		MimeType:  mimeType,
		Size:      int64(len(data)),
		CreatedAt: time.Now().Unix(),
		UserID:    uploaderID,
		// ChatID depends on usage. For avatar upload it's empty, for image upload in chat we could pass it.
		// For now it conforms to existing behavior where it is empty.
		ChatID: "",
	}

	// Thumbnail failure must never fail the upload.
	if _, err := images.AttachThumbnail(a.storage, &meta, data); err != nil {
		slog.Warn("thumbnail generation failed", "fileID", fileID, "error", err)
	}

	if err := a.storage.UpsertFileMetadata(meta); err != nil {
		slog.Error("failed to save file metadata", "error", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return "", err
	}

	return fileID, nil
}

func (a *API) UploadImageHandler(w http.ResponseWriter, r *http.Request) {
	// Limit image
	fileID, err := a.processUpload(w, r, a.cfg.MaxImageSize, true)
	if err != nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(models.UploadImageResponse{ID: fileID}); err != nil {
		slog.Error("failed to encode upload response", "error", err)
	}
}

func (a *API) UploadAvatarHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	uploaderID := user.ID

	// Limit for avatars
	fileID, err := a.processUpload(w, r, a.cfg.MaxAvatarSize, true)
	if err != nil {
		return
	}

	// Avatars are rendered small everywhere, so serve the thumbnail.
	// Serving falls back to the original when no thumbnail exists.
	avatarURL := fmt.Sprintf("/api/images/%s?thumb=1", fileID)
	if err := a.auth.UpdateAvatarURL(uploaderID, avatarURL); err != nil {
		slog.Error("failed to update user avatar url", "error", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}

	// Optionally we could broadcast presence so other clients get the new avatar.
	// For now, updating the database is sufficient as clients fetch user lists periodically or at start.
	// Alternatively we can use a server message type.

	w.Header().Set("Content-Type", "application/json")
	resp := struct {
		AvatarURL string `json:"avatarUrl"`
	}{
		AvatarURL: avatarURL,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode upload avatar response", "error", err)
	}
}

func (a *API) UpdateDisplayNameHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		DisplayName string `json:"displayName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sanitizedName, err := a.auth.UpdateDisplayName(user.ID, req.DisplayName)
	if err != nil {
		msg := "Internal Server Error"
		code := http.StatusInternalServerError
		switch {
		case errors.Is(err, models.ErrNotFound):
			msg = "User not found"
			code = http.StatusNotFound

		case errors.Is(err, auth.ErrEmptyDisplayName):
			msg = err.Error()
			code = http.StatusBadRequest

		}

		if code >= http.StatusInternalServerError {
			slog.Error("failed to update user display name", "error", err)
		}
		http.Error(w, msg, code)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp := struct {
		Success     bool   `json:"success"`
		DisplayName string `json:"displayName"`
	}{
		Success:     true,
		DisplayName: content.Escape(sanitizedName),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode update display name response", "error", err)
	}

	// We can broadcast the change if needed, but for now we follow Avatar updating behavior.
}

func (a *API) UpdateBioHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Bio string `json:"bio"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sanitizedBio, err := a.auth.UpdateBio(user.ID, req.Bio)
	if err != nil {
		if errors.Is(err, auth.ErrBioTooLong) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, models.ErrNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to update user bio", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if updatedUser, err := a.auth.GetUser(user.ID); err == nil {
		go a.hub.BroadcastNewUser(updatedUser)
	}

	w.Header().Set("Content-Type", "application/json")
	resp := struct {
		Success bool   `json:"success"`
		Bio     string `json:"bio"`
	}{
		Success: true,
		Bio:     content.Escape(sanitizedBio),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode update bio response", "error", err)
	}
}

func (a *API) UpdateSongHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var songURL, songTitle, songArtist string
	var err error

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		songURL, songTitle, songArtist, err = a.handleSongUpload(w, r, user.ID)
	} else {
		songURL, songTitle, songArtist, err = a.handleSongJSON(w, r)
	}
	if err != nil {
		return
	}

	if err := a.auth.UpdateProfileSong(user.ID, songURL, songTitle, songArtist); err != nil {
		slog.Error("failed to update user profile song", "error", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}

	if updatedUser, err := a.auth.GetUser(user.ID); err == nil {
		go a.hub.BroadcastNewUser(updatedUser)
	}

	w.Header().Set("Content-Type", "application/json")
	resp := struct {
		Success    bool   `json:"success"`
		SongURL    string `json:"songUrl"`
		SongTitle  string `json:"songTitle"`
		SongArtist string `json:"songArtist"`
	}{
		Success:    true,
		SongURL:    songURL,
		SongTitle:  content.Escape(songTitle),
		SongArtist: content.Escape(songArtist),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode update profile song response", "error", err)
	}
}

func (a *API) handleSongUpload(w http.ResponseWriter, r *http.Request, userID string) (string, string, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxFileSize)
	if err := r.ParseMultipartForm(a.cfg.MaxFileSize); err != nil {
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return "", "", "", err
	}

	songTitle := r.FormValue("title")
	if songTitle == "" {
		songTitle = r.FormValue("songTitle")
	}
	songArtist := r.FormValue("artist")
	if songArtist == "" {
		songArtist = r.FormValue("songArtist")
	}
	songURL := r.FormValue("url")
	if songURL == "" {
		songURL = r.FormValue("songUrl")
	}

	file, header, err := r.FormFile("file")
	if err != nil || file == nil {
		return songURL, songTitle, songArtist, nil
	}
	defer func() { _ = file.Close() }()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
		return "", "", "", err
	}
	data := buf.Bytes()

	if songTitle == "" || songArtist == "" {
		meta := audio.ExtractMetadata(data)
		if songTitle == "" && meta.Title != "" {
			songTitle = meta.Title
		}
		if songArtist == "" && meta.Artist != "" {
			songArtist = meta.Artist
		}
	}

	mimeType := audio.NormalizeMimeType(header.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		if detected := audio.DetectAudioMimeType(data); detected != "" {
			mimeType = detected
		} else if kind, err := filetype.Match(data); err == nil && kind != filetype.Unknown {
			mimeType = audio.NormalizeMimeType(kind.MIME.Value)
		} else {
			mimeType = "audio/mpeg"
		}
	}

	hasher := sha256.New()
	hasher.Write(data)
	hash := hex.EncodeToString(hasher.Sum(nil))

	if err := a.storage.SaveFileBlob(bytes.NewReader(data), hash); err != nil {
		slog.Error("failed to save file blob", "error", err)
		http.Error(w, "Internal Storage Error", http.StatusInternalServerError)
		return "", "", "", err
	}

	fileID := uuid.NewString()
	fileMeta := storage.FileMetadata{
		ID:        fileID,
		Hash:      hash,
		MimeType:  mimeType,
		Size:      int64(len(data)),
		CreatedAt: time.Now().Unix(),
		UserID:    userID,
	}

	if err := a.storage.UpsertFileMetadata(fileMeta); err != nil {
		slog.Error("failed to save file metadata", "error", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return "", "", "", err
	}

	return fmt.Sprintf("/api/files/%s", fileID), songTitle, songArtist, nil
}

func (a *API) handleSongJSON(w http.ResponseWriter, r *http.Request) (string, string, string, error) {
	var req struct {
		SongURL    string `json:"songUrl"`
		SongTitle  string `json:"songTitle"`
		SongArtist string `json:"songArtist"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return "", "", "", err
	}

	if req.SongURL != "" {
		if len(req.SongURL) > 2048 {
			http.Error(w, "URL too long", http.StatusBadRequest)
			return "", "", "", errors.New("URL too long")
		}
		if _, err := url.Parse(req.SongURL); err != nil {
			http.Error(w, "Invalid URL", http.StatusBadRequest)
			return "", "", "", err
		}
	}

	return req.SongURL, req.SongTitle, req.SongArtist, nil
}

func (a *API) DeleteSongHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := a.auth.UpdateProfileSong(user.ID, "", "", ""); err != nil {
		slog.Error("failed to delete profile song", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if updatedUser, err := a.auth.GetUser(user.ID); err == nil {
		go a.hub.BroadcastNewUser(updatedUser)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) GetImageHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Missing file ID", http.StatusBadRequest)
		return
	}

	meta, err := a.storage.GetFileMetadata(id)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Thumbnails are generated before a file ID is ever served (synchronously
	// on upload, blocking migration on start), so a given URL's content never
	// changes and the immutable cache header below stays correct. Files
	// without a thumbnail (SVG, small or undecodable images) fall back to the
	// original.
	hash, mimeType, size := meta.Hash, meta.MimeType, meta.Size
	if r.URL.Query().Get("thumb") == "1" && meta.ThumbnailHash != "" {
		hash, mimeType, size = meta.ThumbnailHash, meta.ThumbnailMime, meta.ThumbnailSize
	}

	rc, err := a.storage.GetFileBlob(hash)
	if err != nil {
		slog.Error("failed to retrieve file blob", "error", err)
		http.Error(w, "File content missing", http.StatusInternalServerError)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")

	if _, err := io.Copy(w, rc); err != nil {
		slog.Error("failed to write file content", "error", err)
	}
}

func (a *API) UploadFileHandler(w http.ResponseWriter, r *http.Request) {
	// Limit for files
	fileID, err := a.processUpload(w, r, a.cfg.MaxFileSize, false)
	if err != nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(models.UploadFileResponse{ID: fileID}); err != nil {
		slog.Error("failed to encode upload response", "error", err)
	}
}

func (a *API) GetFileHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Missing file ID", http.StatusBadRequest)
		return
	}

	meta, err := a.storage.GetFileMetadata(id)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	rc, err := a.storage.GetFileBlob(meta.Hash)
	if err != nil {
		slog.Error("failed to retrieve file blob", "error", err)
		http.Error(w, "File content missing", http.StatusInternalServerError)
		return
	}
	defer func() { _ = rc.Close() }()

	name := r.URL.Query().Get("name")
	if name == "" {
		name = id
	}

	mimeType := audio.NormalizeMimeType(meta.MimeType)
	if seeker, ok := rc.(io.ReadSeeker); ok {
		var headerBuf [512]byte
		n, err := io.ReadFull(seeker, headerBuf[:])
		if (err == nil || errors.Is(err, io.ErrUnexpectedEOF)) && n > 0 {
			if detected := audio.DetectAudioMimeType(headerBuf[:n]); detected != "" {
				mimeType = detected
			} else if mimeType == "" || mimeType == "application/octet-stream" {
				if kind, err := filetype.Match(headerBuf[:n]); err == nil && kind != filetype.Unknown {
					mimeType = audio.NormalizeMimeType(kind.MIME.Value)
				}
			}
		}
		_, _ = seeker.Seek(0, io.SeekStart)
	}

	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	nameWithExt := name
	if filepath.Ext(nameWithExt) == "" {
		if ext := getExtensionForMime(mimeType); ext != "" {
			nameWithExt += ext
		}
	}

	isDownload := r.URL.Query().Get("download") == "1"
	if isDownload {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", nameWithExt))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", nameWithExt))
	}

	if seeker, ok := rc.(io.ReadSeeker); ok {
		modTime := time.Unix(meta.CreatedAt, 0)
		http.ServeContent(w, r, nameWithExt, modTime, seeker)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
		if _, err := io.Copy(w, rc); err != nil {
			slog.Error("failed to write file content", "error", err)
		}
	}
}

func getExtensionForMime(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/mp4":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "audio/flac":
		return ".flac"
	case "audio/aac":
		return ".aac"
	case "audio/webm":
		return ".webm"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ""
	}
}

func (a *API) WebhookHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if user.TargetChatID == "" {
		http.Error(w, "Webhook has no target chat configured", http.StatusBadRequest)
		return
	}

	var req struct {
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.Content = content.Sanitize(req.Content)
	if req.Content == "" {
		http.Error(w, "Message content cannot be empty", http.StatusBadRequest)
		return
	}

	a.hub.Dispatch(user.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  user.TargetChatID,
		Content: req.Content,
	}, nil)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
	})
}

func (a *API) PushVAPIDPublicKeyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write([]byte(a.push.PublicKey())) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter
}

func (a *API) PushSubscribeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var sub struct {
		Endpoint string          `json:"endpoint"`
		Keys     json.RawMessage `json:"keys"`
	}

	// Limit request body size to 10KB
	r.Body = http.MaxBytesReader(w, r.Body, 10240)

	// Read raw body to save it as is
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	if err := json.Unmarshal(body, &sub); err != nil {
		http.Error(w, "Invalid subscription JSON", http.StatusBadRequest)
		return
	}

	if sub.Endpoint == "" {
		http.Error(w, "Missing endpoint", http.StatusBadRequest)
		return
	}

	if err := a.storage.UpsertPushSubscription(user.ID, sub.Endpoint, body); err != nil {
		slog.Error("failed to save push subscription", "error", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
