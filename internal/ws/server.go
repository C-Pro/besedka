package ws

import (
	"besedka/internal/models"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

func HandleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

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
			if msg.ChatID == "townhall" {
				messages = []models.Message{
					{Timestamp: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), UserID: "1", Content: "Hello everyone!"},
					{Timestamp: time.Now().Format(time.RFC3339), UserID: "2", Content: "Hi Alice!"},
				}
			} else if msg.ChatID == "dm_1_2" {
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
