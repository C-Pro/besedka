package ws

import (
	"besedka/internal/auth"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"

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
		log.Printf("unauthorized websocket connection attempt")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// nosemgrep
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("error upgrading to websocket: %v", err)
		return
	}

	defer func() {
		if err := ws.Close(); err != nil {
			if !errors.Is(err, net.ErrClosed) && !strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("error closing websocket: %v", err)
			}
		}
	}()

	// Create Connection
	conn := NewConnection(s.hub, ws, userID)

	// Handle connection (blocks until closed)
	if err := conn.Handle(r.Context()); err != nil {
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			return
		}
		if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
			return
		}
		log.Printf("connection handler error: %v", err)
	}
}
