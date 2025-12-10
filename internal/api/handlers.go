package api

import (
	"besedka/internal/stubs"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Dummy token generation
	token := "dummy_jwt_token_" + time.Now().Format(time.RFC3339)

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		HttpOnly: true,
		Path:     "/",
	})

	w.Header().Set("Content-Type", "application/json")
	// Return dummy user ID "1" (Alice) for now
	// Return dummy user ID "1" (Alice) for now
	if err := json.NewEncoder(w).Encode(map[string]string{
		"token":  token,
		"userId": "1",
	}); err != nil {
		log.Printf("failed to encode login response: %v", err)
	}
}

func LogoffHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
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

func UsersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stubs.Users); err != nil {
		log.Printf("failed to encode users response: %v", err)
	}
}

func ChatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stubs.Chats); err != nil {
		log.Printf("failed to encode chats response: %v", err)
	}
}
