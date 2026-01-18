package http

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/storage"
	"besedka/internal/ws"
	"context"
	"log"
	"net/http"
	"sync"
)

type APIServer struct {
	server *http.Server
	wg     sync.WaitGroup
}

func NewAPIServer(authService *auth.AuthService, bbStorage *storage.BboltStorage, addr string) *APIServer {
	// Initialize Hub
	hub := ws.NewHub(authService, bbStorage)

	server := ws.NewServer(authService, hub)
	apiHandlers := api.New(authService, hub)

	mux := http.NewServeMux()

	// Serve static files with Auth check
	mux.HandleFunc("/", NewFileServerHandler(authService, "."))

	// API endpoints
	mux.HandleFunc("/api/login", apiHandlers.LoginHandler)
	mux.HandleFunc("/api/register", apiHandlers.RegisterHandler)
	mux.HandleFunc("/api/logoff", apiHandlers.LogoffHandler)
	mux.HandleFunc("/api/users", apiHandlers.UsersHandler)
	mux.HandleFunc("/api/chats", apiHandlers.ChatsHandler)
	mux.HandleFunc("/api/me", apiHandlers.MeHandler)

	// WebSocket endpoint
	mux.HandleFunc("/api/chat", server.HandleConnections)

	if addr == "" {
		addr = ":8080"
	}

	return &APIServer{
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

func (s *APIServer) Start() error {
	log.Printf("Server started on %s", s.server.Addr)
	s.wg.Add(1)
	defer s.wg.Done()

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *APIServer) Shutdown(ctx context.Context) error {
	defer s.wg.Wait()
	return s.server.Shutdown(ctx)
}
