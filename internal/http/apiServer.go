package http

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/filestore"
	"besedka/internal/storage"
	"besedka/internal/ws"
	"besedka/static"
	"context"
	"log"
	"net/http"
	"sync"
)

type APIServer struct {
	server *http.Server
	wg     sync.WaitGroup
}

func NewAPIServer(authService *auth.AuthService, hub *ws.Hub, filestore filestore.FileStore, storage *storage.BboltStorage, addr string) *APIServer {
	// Initialize Hub
	// hub := ws.NewHub(authService, bbStorage)

	server := ws.NewServer(authService, hub)
	apiHandlers := api.New(authService, hub, filestore, storage)

	mux := http.NewServeMux()

	// Serve static files with Auth check
	mux.HandleFunc("/", NewFileServerHandler(authService, static.Content))

	// API endpoints
	log.Printf("Registering API handler: /api/register-info")
	mux.HandleFunc("/api/login", apiHandlers.LoginHandler)
	mux.HandleFunc("/api/logoff", apiHandlers.LogoffHandler)
	mux.HandleFunc("/api/register", apiHandlers.RegisterHandler)
	mux.HandleFunc("/api/register-info", apiHandlers.RegisterInfoHandler)
	mux.HandleFunc("/api/users", apiHandlers.UsersHandler)
	mux.HandleFunc("/api/chats", apiHandlers.ChatsHandler)
	mux.HandleFunc("/api/me", apiHandlers.MeHandler)
	mux.HandleFunc("/api/upload/image", apiHandlers.UploadImageHandler)
	mux.HandleFunc("/api/images/{id}", apiHandlers.GetImageHandler)

	// WebSocket endpoint
	mux.HandleFunc("/api/chat", server.HandleConnections)

	if addr == "" {
		addr = ":8080"
	}

	return &APIServer{
		server: &http.Server{
			Addr: addr,
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
