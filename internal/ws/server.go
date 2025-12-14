package ws

import (
	"besedka/internal/auth"
	"besedka/internal/models"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type Server struct {
	auth     *auth.AuthService
	upgrader *websocket.Upgrader
}

func NewServer(auth *auth.AuthService) *Server {
	return &Server{
		auth: auth,
		upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

func (s *Server) HandleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := s.auth.GetUserID(r.Header.Get("token"))
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("error upgrading to websocket: %v", err)
		return
	}

	defer func() {
		if err := ws.Close(); err != nil {
			log.Printf("error closing websocket: %v", err)
		}
	}()

	for {
		// Read message from client
		var msg models.ClientMessage
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("error: %v", err)
			break
		}

		log.Printf("Received message: %+v", msg)

		if msg.Type == models.ClientMessageTypeJoin {
			// Send stub messages for the joined chat

			var messages []models.Message
			switch msg.ChatID {
			case "townhall":
				messages = []models.Message{
					{Timestamp: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), UserID: "1", Content: "Hello everyone!"},
					{Timestamp: time.Now().Format(time.RFC3339), UserID: "2", Content: "Hi Alice!"},
				}
			case "dm_1_2":
				messages = []models.Message{
					{Timestamp: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), UserID: "1", Content: "Hello Alice!"},
					{Timestamp: time.Now().Format(time.RFC3339), UserID: "2", Content: "Hi User!"},
				}
			}

			if len(messages) > 0 {
				response := models.ServerMessage{
					Type:     models.ServerMessageTypeMessages,
					ChatID:   msg.ChatID,
					Messages: messages,
				}
				if err := ws.WriteJSON(response); err != nil {
					log.Printf("error: %v", err)
					break
				}
			}
		} else if msg.Type == models.ClientMessageTypeSend {
			response := models.ServerMessage{
				Type:   models.ServerMessageTypeMessages,
				ChatID: msg.ChatID,
				Messages: []models.Message{
					{Timestamp: time.Now().Format(time.RFC3339), UserID: "me", Content: msg.Content},
				},
			}
			if err := ws.WriteJSON(response); err != nil {
				log.Printf("error: %v", err)
				break
			}
		}
	}
}
