package api

import (
	"besedka/internal/auth"
	"besedka/internal/stubs"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type API struct {
	auth *auth.AuthService
}

func New(auth *auth.AuthService) *API {
	return &API{auth: auth}
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

	resp := a.auth.Register(req)
	if !resp.Success {
		http.Error(w, resp.Message, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to encode register response: %v", err)
	}
}

func (a *API) UsersHandler(w http.ResponseWriter, r *http.Request) {
	token := a.getToken(r)
	if _, err := a.auth.GetUserID(token); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stubs.Users); err != nil {
		log.Printf("failed to encode users response: %v", err)
	}
}

func (a *API) ChatsHandler(w http.ResponseWriter, r *http.Request) {
	token := a.getToken(r)
	if _, err := a.auth.GetUserID(token); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stubs.Chats); err != nil {
		log.Printf("failed to encode chats response: %v", err)
	}
}
