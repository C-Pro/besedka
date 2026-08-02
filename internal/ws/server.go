package ws

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"besedka/internal/auth"
	"besedka/internal/models"

	"github.com/gorilla/websocket"
)

type Server struct {
	auth     *auth.AuthService
	upgrader *websocket.Upgrader
	hub      *Hub
}

func NewServer(auth *auth.AuthService, hub *Hub) *Server {
	return &Server{
		auth:     auth,
		upgrader: &websocket.Upgrader{},
		hub:      hub,
	}
}

func (s *Server) HandleConnections(w http.ResponseWriter, r *http.Request) {
	var userID string
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		apiKey := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if u, err := s.auth.GetUserByAPIKey(apiKey); err == nil {
			if u.Type == models.UserTypeWebhook {
				http.Error(w, "Webhooks cannot connect via WebSocket", http.StatusForbidden)
				return
			}
			userID = u.ID
		}
	}

	token := r.Header.Get("token")
	if userID == "" {
		if token == "" {
			if c, err := r.Cookie("token"); err == nil {
				token = c.Value
			}
		}
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if strings.HasPrefix(token, "bsk_") {
			if u, err := s.auth.GetUserByAPIKey(token); err == nil {
				if u.Type == models.UserTypeWebhook {
					http.Error(w, "Webhooks cannot connect via WebSocket", http.StatusForbidden)
					return
				}
				userID = u.ID
			}
		}

		if userID == "" {
			id, _, err := s.auth.GetUserID(token)
			if err != nil {
				slog.Warn("unauthorized websocket connection attempt")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			userID = id
		}
	}

	u, err := s.auth.GetUser(userID)
	if err == nil && u.Type == models.UserTypeWebhook {
		http.Error(w, "Webhooks cannot connect via WebSocket", http.StatusForbidden)
		return
	}

	// Force refresh token/session in-memory and cookie on upgrade
	var responseHeader http.Header
	if token != "" {
		if expiry, err := s.auth.RefreshToken(token); err == nil {
			responseHeader = http.Header{}
			responseHeader.Add("Set-Cookie", (&http.Cookie{
				Name:     "token",
				Value:    token,
				HttpOnly: true,
				Secure:   true,
				Path:     "/",
				Expires:  expiry,
			}).String())
		}
	}

	// nosemgrep
	ws, err := s.upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		slog.Error("error upgrading to websocket", "address", r.RemoteAddr, "error", err)
		return
	}

	// Create Connection
	conn := NewConnection(s.hub, ws, userID)

	// Handle connection (blocks until closed)
	if err := conn.Handle(r.Context()); err != nil {
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			return
		}
		if errors.Is(err, net.ErrClosed) {
			return
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return
		}
		slog.Error("connection handler failed", "error", err)
	}
}
