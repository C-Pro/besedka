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
	"strconv"
	"time"

	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/content"
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

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    loginResp.Token,
		HttpOnly: true,
		Secure:   true,
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

const userIDKey = contextKey("userID")

func UserIDFromContext(ctx context.Context) string {
	userID, _ := ctx.Value(userIDKey).(string)
	return userID
}

func (a *API) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := a.getToken(r)
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID, err := a.auth.GetUserID(token)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
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

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    resp.Token,
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		Expires:  time.Now().Add(auth.DefaultTokenExpiry), // Or use configured expiry
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
		users[i].Presence.Online = a.hub.IsUserOnline(users[i].ID)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		slog.Error("failed to encode users response", "error", err)
	}
}

func (a *API) ChatsHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	chats := a.hub.GetChats(userID)

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

	userID := UserIDFromContext(r.Context())

	messages, err := a.hub.GetChatRecords(userID, chatID, fromSeq, toSeq)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			http.Error(w, "Chat not found or access denied", http.StatusForbidden)
		} else {
			slog.Error("failed to get chat records", "error", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(messages); err != nil {
		slog.Error("failed to encode messages response", "error", err)
	}
}

func (a *API) MeHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	currentUser, err := a.auth.GetUser(userID)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Return a simplified structure or the full user struct.
	// The frontend expects { id: ... } at minimum based on existing logic,
	// but having name is good too.
	resp := struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{
		ID:   currentUser.ID,
		Name: content.Escape(currentUser.DisplayName),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode me response", "error", err)
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
func RequireSameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !validateCSRFSameOrigin(r) {
				http.Error(w, "Invalid Origin", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func (a *API) ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	regToken, err := a.auth.ResetPassword(userID)
	if err != nil {
		slog.Error("failed to reset password for user", "userID", userID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Message: "Failed to reset password",
		})
		return
	}

	// Make sure the resetting user gets logged out from all other active sessions and websockets immediately
	a.hub.DisconnectUser(userID) // This disconnects all ws connections

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

func (a *API) processUpload(w http.ResponseWriter, r *http.Request, maxBytes int64, enforceImage bool) (string, error) {
	uploaderID := UserIDFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return "", err
	}
	data := buf.Bytes()

	if enforceImage {
		if !filetype.IsImage(data) {
			http.Error(w, "Invalid file type. Only images are allowed.", http.StatusBadRequest)
			return "", errors.New("invalid file type")
		}
	}

	mimeType := "application/octet-stream"
	kind, err := filetype.Match(data)
	if err == nil && kind != filetype.Unknown {
		mimeType = kind.MIME.Value
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
	uploaderID := UserIDFromContext(r.Context())

	// Limit for avatars
	fileID, err := a.processUpload(w, r, a.cfg.MaxAvatarSize, true)
	if err != nil {
		return
	}

	avatarURL := fmt.Sprintf("/api/images/%s", fileID)
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
	userID := UserIDFromContext(r.Context())

	var req struct {
		DisplayName string `json:"displayName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sanitizedName, err := a.auth.UpdateDisplayName(userID, req.DisplayName)
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

	rc, err := a.storage.GetFileBlob(meta.Hash)
	if err != nil {
		slog.Error("failed to retrieve file blob", "error", err)
		http.Error(w, "File content missing", http.StatusInternalServerError)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
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

	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(w, rc); err != nil {
		slog.Error("failed to write file content", "error", err)
	}
}

func (a *API) PushVAPIDPublicKeyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write([]byte(a.push.PublicKey())) //nosemgrep
}

func (a *API) PushSubscribeHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

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

	if err := a.storage.UpsertPushSubscription(userID, sub.Endpoint, body); err != nil {
		slog.Error("failed to save push subscription", "error", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
