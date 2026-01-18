package http

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"context"
	"log"
	"net/http"
	"sync"
)

type AdminServer struct {
	server *http.Server
	wg     sync.WaitGroup
}

func NewAdminServer(authService *auth.AuthService, addr string) *AdminServer {
	adminHandler := api.NewAdminHandler(authService)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users", adminHandler.AddUserHandler)

	if addr == "" {
		addr = "localhost:8081"
	}

	return &AdminServer{
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

func (s *AdminServer) Start() error {
	log.Printf("Admin API started on %s", s.server.Addr)
	s.wg.Add(1)
	defer s.wg.Done()

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *AdminServer) Shutdown(ctx context.Context) error {
	defer s.wg.Wait()
	return s.server.Shutdown(ctx)
}
