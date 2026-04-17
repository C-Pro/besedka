package ws

import (
	"errors"
	"log/slog"
	"net"
	"net/http"

	"besedka/internal/auth"

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
	token := r.Header.Get("token")
	if token == "" {
		if c, err := r.Cookie("token"); err == nil {
			token = c.Value
		}
	}

	userID, err := s.auth.GetUserID(token)
	if err != nil {
		slog.Warn("unauthorized websocket connection attempt")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// nosemgrep
	ws, err := s.upgrader.Upgrade(w, r, nil)
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
