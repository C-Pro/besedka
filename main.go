package main

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/storage"
	"besedka/internal/stubs"
	"besedka/internal/ws"
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"strings"
	"time"
)

func rootHandler(authService *auth.AuthService, fs http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// public paths that don't need auth
		publicPaths := []string{"/login.html", "/register.html", "/css/", "/js/"}

		// If it matches a public path prefix, serve it directly
		for _, prefix := range publicPaths {
			if strings.HasPrefix(r.URL.Path, prefix) {
				fs.ServeHTTP(w, r)
				return
			}
		}

		// Specific check for exactly root "/" or "/index.html"
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			// Check cookie
			cookie, err := r.Cookie("token")
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login.html", http.StatusFound)
				return
			}

			// Validate token
			if _, err := authService.GetUserID(cookie.Value); err != nil {
				http.Redirect(w, r, "/login.html", http.StatusFound)
				return
			}
		}

		// Default to file server
		fs.ServeHTTP(w, r)
	}
}

func main() {
	// Initialize services
	ctx := context.Background()
	authConfig := auth.Config{
		Secret:      base64.StdEncoding.EncodeToString([]byte("very-secure-secret-key-for-development-mode")),
		TokenExpiry: 24 * time.Hour,
	}

	bbStorage, err := storage.NewBboltStorage("besedka.db")
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer func() { _ = bbStorage.Close() }()

	authService, err := auth.NewAuthService(ctx, authConfig, bbStorage)
	if err != nil {
		log.Fatalf("Failed to initialize auth service: %v", err)
	}

	// Seed users from stubs
	for _, u := range stubs.Users {
		// Default password is "password"
		_, err := authService.SeedUser(u.ID, u.DisplayName, "password", u.DisplayName, u.AvatarURL)
		if err != nil {
			log.Printf("Warning: failed to seed user %s: %v", u.DisplayName, err)
		}
	}

	// Serve static files with Auth check
	fs := http.FileServer(http.Dir("."))
	http.HandleFunc("/", rootHandler(authService, fs))

	// Initialize Hub
	hub := ws.NewHub(authService, bbStorage)

	server := ws.NewServer(authService, hub)
	apiHandlers := api.New(authService, hub)

	// API endpoints
	http.HandleFunc("/api/login", apiHandlers.LoginHandler)
	http.HandleFunc("/api/register", apiHandlers.RegisterHandler)
	http.HandleFunc("/api/logoff", apiHandlers.LogoffHandler)
	http.HandleFunc("/api/users", apiHandlers.UsersHandler)
	http.HandleFunc("/api/chats", apiHandlers.ChatsHandler)
	http.HandleFunc("/api/me", apiHandlers.MeHandler)

	// WebSocket endpoint
	http.HandleFunc("/api/chat", server.HandleConnections)

	log.Println("Server started on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
