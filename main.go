package main

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/stubs"
	"besedka/internal/ws"
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"time"
)

func main() {
	// Serve static files
	fs := http.FileServer(http.Dir("."))
	http.Handle("/", fs)

	// Initialize services
	ctx := context.Background()
	authConfig := auth.Config{
		Secret:      base64.StdEncoding.EncodeToString([]byte("very-secure-secret-key-for-development-mode")),
		TokenExpiry: 24 * time.Hour,
	}
	authService, err := auth.NewAuthService(ctx, authConfig)
	if err != nil {
		log.Fatalf("Failed to initialize auth service: %v", err)
	}

	// Seed users from stubs
	for _, u := range stubs.Users {
		// Default password is "password"
		_, err := authService.SeedUser(u.ID, u.DisplayName, "password")
		if err != nil {
			log.Printf("Warning: failed to seed user %s: %v", u.DisplayName, err)
		}
	}

	// Initialize Hub
	hub := ws.NewHub()

	server := ws.NewServer(authService, hub)
	apiHandlers := api.New(authService, hub)

	// API endpoints
	http.HandleFunc("/api/login", apiHandlers.LoginHandler)
	http.HandleFunc("/api/register", apiHandlers.RegisterHandler)
	http.HandleFunc("/api/logoff", apiHandlers.LogoffHandler)
	http.HandleFunc("/api/users", apiHandlers.UsersHandler)
	http.HandleFunc("/api/chats", apiHandlers.ChatsHandler)

	// WebSocket endpoint
	http.HandleFunc("/api/chat", server.HandleConnections)

	log.Println("Server started on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
