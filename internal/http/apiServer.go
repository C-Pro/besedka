package http

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"

	"besedka/internal/api"
	"besedka/internal/auth"
	"besedka/internal/certmanager"
	"besedka/internal/config"
	"besedka/internal/models"
	"besedka/internal/objectstore"
	"besedka/internal/push"
	"besedka/internal/storage"
	"besedka/internal/ws"
)

type APIServer struct {
	server              *http.Server
	httpChallengeServer *http.Server
	cfg                 *config.Config
	wg                  sync.WaitGroup
}

func NewAPIServer(cfg *config.Config, authService *auth.AuthService, hub *ws.Hub, storage *storage.BboltStorage, pushService *push.Service, addr string, assets fs.FS) *APIServer {
	// Initialize Hub
	// hub := ws.NewHub(authService, bbStorage)

	server := ws.NewServer(authService, hub)
	apiHandlers := api.New(authService, hub, storage, cfg, pushService)

	mux := http.NewServeMux()

	// Serve static files with Auth check
	mux.HandleFunc("/", NewFileServerHandler(authService, assets))

	// API endpoints
	mux.HandleFunc("POST /api/login", api.RequireSameOrigin(apiHandlers.LoginHandler))
	mux.HandleFunc("POST /api/logoff", api.RequireSameOrigin(apiHandlers.LogoffHandler))
	mux.HandleFunc("POST /api/register", api.RequireSameOrigin(apiHandlers.RegisterHandler))
	mux.HandleFunc("GET /api/register-info", apiHandlers.RegisterInfoHandler)
	mux.HandleFunc("POST /api/reset-password", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.ResetPasswordHandler)))
	mux.HandleFunc("GET /api/users", apiHandlers.RequireAuth(apiHandlers.UsersHandler))
	mux.HandleFunc("GET /api/chats", apiHandlers.RequireAuth(apiHandlers.ChatsHandler))
	mux.HandleFunc("GET /api/chats/{id}/messages", apiHandlers.RequireAuth(api.RequireUserTypes(apiHandlers.ChatMessagesHandler, models.UserTypeHuman, models.UserTypeBot)))
	mux.HandleFunc("POST /api/chats/{id}/messages", apiHandlers.RequireAuth(api.RequireSameOrigin(api.RequireUserTypes(apiHandlers.SendMessageHandler, models.UserTypeHuman, models.UserTypeBot))))
	mux.HandleFunc("GET /api/me", apiHandlers.RequireAuth(apiHandlers.MeHandler))
	mux.HandleFunc("POST /api/users/me/avatar", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.UploadAvatarHandler)))
	mux.HandleFunc("POST /api/users/me/display-name", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.UpdateDisplayNameHandler)))
	mux.HandleFunc("POST /api/users/me/bio", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.UpdateBioHandler)))
	mux.HandleFunc("POST /api/users/me/song", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.UpdateSongHandler)))
	mux.HandleFunc("DELETE /api/users/me/song", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.DeleteSongHandler)))
	mux.HandleFunc("GET /api/users/me/settings", apiHandlers.RequireAuth(apiHandlers.GetUserSettingsHandler))
	mux.HandleFunc("POST /api/users/me/settings", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.UpdateUserSettingsHandler)))
	mux.HandleFunc("POST /api/upload/image", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.UploadImageHandler)))
	mux.HandleFunc("POST /api/upload/file", apiHandlers.RequireAuth(api.RequireSameOrigin(apiHandlers.UploadFileHandler)))
	mux.HandleFunc("GET /api/images/{id}", apiHandlers.RequireAuth(apiHandlers.GetImageHandler))
	mux.HandleFunc("GET /api/files/{id}", apiHandlers.RequireAuth(apiHandlers.GetFileHandler))
	mux.HandleFunc("POST /api/webhook", apiHandlers.RequireAuth(api.RequireSameOrigin(api.RequireUserTypes(apiHandlers.WebhookHandler, models.UserTypeWebhook))))

	// Push notification endpoints
	mux.HandleFunc("GET /api/push/vapidPublicKey", apiHandlers.RequireAuth(apiHandlers.PushVAPIDPublicKeyHandler))

	// WebAuthn endpoints
	mux.HandleFunc("POST /api/webauthn/register/begin", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.WebAuthnRegisterBeginHandler)))
	mux.HandleFunc("POST /api/webauthn/register/finish", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.WebAuthnRegisterFinishHandler)))
	mux.HandleFunc("POST /api/webauthn/login/begin", api.RequireSameOrigin(apiHandlers.WebAuthnLoginBeginHandler))
	mux.HandleFunc("POST /api/webauthn/login/finish", api.RequireSameOrigin(apiHandlers.WebAuthnLoginFinishHandler))
	mux.HandleFunc("GET /api/webauthn/passkeys", apiHandlers.RequireAuth(apiHandlers.ListPasskeysHandler))
	mux.HandleFunc("DELETE /api/webauthn/passkeys/{id}", api.RequireSameOrigin(apiHandlers.RequireAuth(apiHandlers.DeletePasskeyHandler)))
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
		var obj *objectstore.Client
		if s.cfg.S3Enabled() {
			var err error
			obj, err = objectstore.New(objectstore.Config{
				Endpoint:  s.cfg.S3Endpoint,
				Region:    s.cfg.S3Region,
				Bucket:    s.cfg.S3Bucket,
				AccessKey: s.cfg.S3AccessKey,
				SecretKey: s.cfg.S3SecretKey,
				PathStyle: s.cfg.S3PathStyle,
			})
			if err != nil {
				return fmt.Errorf("failed to initialize objectstore for certmanager: %w", err)
			}
		}

		cm, err := certmanager.New(s.cfg, obj)
		if err != nil {
			return fmt.Errorf("failed to initialize certmanager: %w", err)
		}

		if err := cm.Init(context.Background()); err != nil {
			slog.Warn("certmanager init warning", "error", err)
		}

		s.server.TLSConfig = cm.TLSConfig()

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
				Handler: cm.HTTPHandler(http.HandlerFunc(s.httpsRedirectFallbackHandler)),
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
	http.Redirect(w, r, target, http.StatusTemporaryRedirect) // nosemgrep: go.lang.security.injection.open-redirect.open-redirect
}
