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
	mux.HandleFunc("POST /api/login", api.RequireSameOrigin(apiHandlers.LoginHandler))
	mux.HandleFunc("POST /api/logoff", api.RequireSameOrigin(apiHandlers.LogoffHandler))
	mux.HandleFunc("POST /api/register", api.RequireSameOrigin(apiHandlers.RegisterHandler))
	mux.HandleFunc("GET /api/register-info", apiHandlers.RegisterInfoHandler)
	mux.HandleFunc("POST /api/reset-password", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.ResetPasswordHandler)))
	mux.HandleFunc("GET /api/users", apiHandlers.RequireAuth(apiHandlers.UsersHandler))
	mux.HandleFunc("GET /api/chats", apiHandlers.RequireAuth(apiHandlers.ChatsHandler))
	mux.HandleFunc("GET /api/me", apiHandlers.RequireAuth(apiHandlers.MeHandler))
	mux.HandleFunc("POST /api/users/me/avatar", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.UploadAvatarHandler)))
	mux.HandleFunc("POST /api/users/me/display-name", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.UpdateDisplayNameHandler)))
	mux.HandleFunc("POST /api/upload/image", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.UploadImageHandler)))
	mux.HandleFunc("GET /api/images/{id}", apiHandlers.GetImageHandler)

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
