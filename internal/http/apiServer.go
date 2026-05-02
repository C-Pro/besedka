package http

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"

	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/config"
	"besedka/internal/push"
	"besedka/internal/storage"
	"besedka/internal/ws"
	"besedka/static"

	"golang.org/x/crypto/acme/autocert"
)

type APIServer struct {
	server              *http.Server
	httpChallengeServer *http.Server
	cfg                 *config.Config
	wg                  sync.WaitGroup
}

func NewAPIServer(cfg *config.Config, authService *auth.AuthService, hub *ws.Hub, storage *storage.BboltStorage, pushService *push.Service, addr string) *APIServer {
	// Initialize Hub
	// hub := ws.NewHub(authService, bbStorage)

	server := ws.NewServer(authService, hub)
	apiHandlers := api.New(authService, hub, storage, cfg, pushService)

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
	mux.HandleFunc("GET /api/chats/{id}/messages", apiHandlers.RequireAuth(apiHandlers.ChatMessagesHandler))
	mux.HandleFunc("GET /api/me", apiHandlers.RequireAuth(apiHandlers.MeHandler))
	mux.HandleFunc("POST /api/users/me/avatar", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.UploadAvatarHandler)))
	mux.HandleFunc("POST /api/users/me/display-name", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.UpdateDisplayNameHandler)))
	mux.HandleFunc("POST /api/upload/image", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.UploadImageHandler)))
	mux.HandleFunc("POST /api/upload/file", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.UploadFileHandler)))
	mux.HandleFunc("GET /api/images/{id}", apiHandlers.RequireAuth(apiHandlers.GetImageHandler))
	mux.HandleFunc("GET /api/files/{id}", apiHandlers.RequireAuth(apiHandlers.GetFileHandler))

	// Push notification endpoints
	mux.HandleFunc("GET /api/push/vapidPublicKey", apiHandlers.RequireAuth(apiHandlers.PushVAPIDPublicKeyHandler))
	mux.HandleFunc("POST /api/push/subscribe", apiHandlers.RequireAuth(apiHandlers.PushSubscribeHandler))

	// WebSocket endpoint
	mux.HandleFunc("/api/chat", server.HandleConnections)

	return &APIServer{
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		cfg: cfg,
	}
}

func (s *APIServer) Start() error {
	slog.Info("Server started", "address", s.server.Addr)
	s.wg.Add(1)
	defer s.wg.Done()

	if s.cfg.TLSAutoCertPath != "" {
		hostURL, err := url.Parse(s.cfg.BaseURL)
		if err != nil {
			return err
		}
		hostname := hostURL.Hostname()

		manager := &autocert.Manager{
			Cache:      autocert.DirCache(s.cfg.TLSAutoCertPath),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(hostname),
		}
		s.server.TLSConfig = manager.TLSConfig()

		if s.cfg.EnableHTTPChallenge {
			port := s.cfg.HTTPChallengePort
			if port == "" {
				port = "80"
			}

			host, _, err := net.SplitHostPort(s.server.Addr)
			var challengeAddr string
			if err == nil {
				challengeAddr = net.JoinHostPort(host, port)
			} else {
				// Fallback if SplitHostPort fails (shouldn't happen with valid listen address)
				challengeAddr = ":" + port
			}

			s.httpChallengeServer = &http.Server{
				Addr:    challengeAddr,
				Handler: manager.HTTPHandler(http.HandlerFunc(s.httpsRedirectFallbackHandler)),
			}
			s.wg.Go(func() {
				slog.Info("HTTP challenge server started", "address", s.httpChallengeServer.Addr)
				if err := s.httpChallengeServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					slog.Error("HTTP challenge server error", "error", err)
				}
			})
		}

		if err := s.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	} else if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		if err := s.server.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *APIServer) Shutdown(ctx context.Context) error {
	if s.httpChallengeServer != nil {
		if err := s.httpChallengeServer.Shutdown(ctx); err != nil {
			slog.Error("HTTP challenge server shutdown error", "error", err)
		}
	}
	defer s.wg.Wait()
	return s.server.Shutdown(ctx)
}

func (s *APIServer) httpsRedirectFallbackHandler(w http.ResponseWriter, r *http.Request) {
	u, _ := url.Parse(s.cfg.BaseURL)
	if r.Host != u.Host {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	target := "https://" + r.Host + r.URL.Path
	if len(r.URL.RawQuery) > 0 {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect) //nosemgrep
}
