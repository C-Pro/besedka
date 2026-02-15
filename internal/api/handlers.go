package api

import (
	"besedka/internal/auth"
	"besedka/internal/content"
	"besedka/internal/filestore"
	"besedka/internal/storage"
	"besedka/internal/ws"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/h2non/filetype"
)

type API struct {
	auth      *auth.AuthService
	hub       *ws.Hub
	filestore filestore.FileStore
	storage   *storage.BboltStorage
}

func New(auth *auth.AuthService, hub *ws.Hub, filestore filestore.FileStore, storage *storage.BboltStorage) *API {
	return &API{
		auth:      auth,
		hub:       hub,
		filestore: filestore,
		storage:   storage,
	}
}

func (a *API) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
			log.Printf("failed to encode login response: %v", err)
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
		log.Printf("failed to encode login response: %v", err)
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

func (a *API) LogoffHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req auth.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Sanitize inputs
	req.DisplayName = content.Sanitize(req.DisplayName)

	resp, token := a.auth.CompleteRegistration(req)
	if !resp.Success {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("failed to encode register response: %v", err)
		}
		return
	}

	// Update Hub with new user DMs
	if userID, err := a.auth.GetUserID(token); err == nil {
		if user, err := a.auth.GetUser(userID); err == nil {
			if users, err := a.auth.GetUsers(); err == nil {
				a.hub.EnsureDMsFor(user, users)
			}
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		Expires:  time.Now().Add(auth.DefaultTokenExpiry), // Or use configured expiry
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to encode register response: %v", err)
	}
}

func (a *API) RegisterInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
		log.Printf("failed to encode register info response: %v", err)
	}
}

func (a *API) UsersHandler(w http.ResponseWriter, r *http.Request) {
	token := a.getToken(r)
	if _, err := a.auth.GetUserID(token); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	users, err := a.auth.GetUsers()
	if err != nil {
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}

	// Escape output
	for i := range users {
		users[i].DisplayName = content.Escape(users[i].DisplayName)
		users[i].UserName = content.Escape(users[i].UserName)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		log.Printf("failed to encode users response: %v", err)
	}
}

func (a *API) ChatsHandler(w http.ResponseWriter, r *http.Request) {
	token := a.getToken(r)
	if _, err := a.auth.GetUserID(token); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, _ := a.auth.GetUserID(token) // Error checked above

	chats := a.hub.GetChats(userID)

	// Escape output
	for i := range chats {
		chats[i].Name = content.Escape(chats[i].Name)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(chats); err != nil {
		log.Printf("failed to encode chats response: %v", err)
	}
}

func (a *API) MeHandler(w http.ResponseWriter, r *http.Request) {
	token := a.getToken(r)
	userID, err := a.auth.GetUserID(token)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

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
		log.Printf("failed to encode me response: %v", err)
	}
}

func (a *API) UploadImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := a.getToken(r)
	uploaderID, err := a.auth.GetUserID(token)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Limit to 10MB
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	// Read file content
	// We read to memory to calculate hash and detect type.
	// For very large files, this should be streamed, but 10MB is fine for now.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	data := buf.Bytes()

	// Calculate Hash
	hasher := sha256.New()
	hasher.Write(data)
	hash := hex.EncodeToString(hasher.Sum(nil))

	// Detect MIME type
	kind, err := filetype.Image(data)
	if err != nil {
		http.Error(w, "Invalid file type. Only images are allowed.", http.StatusBadRequest)
		return
	}

	// Save file (idempotent)
	if err := a.filestore.Save(bytes.NewReader(data), hash); err != nil {
		log.Printf("failed to save file blob: %v", err)
		http.Error(w, "Internal Storage Error", http.StatusInternalServerError)
		return
	}

	// Create Metadata
	fileID := uuid.NewString()
	meta := storage.FileMetadata{
		ID:        fileID,
		Hash:      hash,
		MimeType:  kind.MIME.Value,
		Size:      int64(len(data)),
		CreatedAt: time.Now().Unix(),
		UserID:    uploaderID,
		ChatID:    "", // TODO: Pass from request if needed
	}

	if err := a.storage.UpsertFileMetadata(meta); err != nil {
		log.Printf("failed to save file metadata: %v", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"id": fileID}); err != nil {
		log.Printf("failed to encode upload response: %v", err)
	}
}

func (a *API) GetImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	rc, err := a.filestore.Get(meta.Hash)
	if err != nil {
		log.Printf("failed to retrieve file blob: %v", err)
		http.Error(w, "File content missing", http.StatusInternalServerError)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	if _, err := io.Copy(w, rc); err != nil {
		log.Printf("failed to write file content: %v", err)
	}
}
