package ws

import (
	"besedka/internal/chat"
	"besedka/internal/models"
	"besedka/internal/stubs"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Hub struct {
	// Map of chatID -> Chat object
	chats map[string]*chat.Chat

	// Map of userID -> Connection channel
	connectedUsers map[string]chan models.ServerMessage

	// List of all known users (for creating DMs)
	knownUsers map[string]models.User

	mu sync.RWMutex
}

func NewHub() *Hub {
	h := &Hub{
		chats:          make(map[string]*chat.Chat),
		connectedUsers: make(map[string]chan models.ServerMessage),
		knownUsers:     make(map[string]models.User),
	}

	// Create Townhall
	h.createChat("townhall", 100)

	// Populate users from stubs
	for _, u := range stubs.Users {
		h.AddUser(u)
	}

	return h
}

func (h *Hub) createChat(id string, maxRecords int) *chat.Chat {
	c := chat.New(chat.Config{
		ID:             id,
		MaxRecords:     maxRecords,
		RecordCallback: h.handleRecordCallback,
	})
	h.chats[id] = c
	return c
}

func (h *Hub) AddUser(user models.User) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.knownUsers[user.ID]; ok {
		return
	}
	h.knownUsers[user.ID] = user

	// Create DMs with all other existing users
	for otherID := range h.knownUsers {
		if otherID == user.ID {
			continue
		}
		// Create deterministic ID for DM
		dmID := getDMID(user.ID, otherID)
		if _, exists := h.chats[dmID]; !exists {
			h.createChat(dmID, 50) // DM limit 50?
		}
	}
}

func (h *Hub) Join(userID string) chan models.ServerMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	user, ok := h.knownUsers[userID]
	if !ok {
		return nil
	}

	user.Presence = models.Presence{
		Online:   true,
		LastSeen: time.Now().Unix(),
	}
	h.knownUsers[userID] = user

	ch := make(chan models.ServerMessage, 100)
	h.connectedUsers[userID] = ch

	// Join all relevant chats
	// Logic: A user should be part of Townhall and all their DMs
	for chatID, c := range h.chats {
		if chatID == "townhall" || isUserInDM(userID, chatID) {
			c.Join(userID)
		}
	}

	return ch
}

func (h *Hub) Leave(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	user, ok := h.knownUsers[userID]
	if !ok {
		return
	}

	user.Presence = models.Presence{
		Online:   false,
		LastSeen: time.Now().Unix(),
	}
	h.knownUsers[userID] = user

	if ch, ok := h.connectedUsers[userID]; ok {
		close(ch)
		delete(h.connectedUsers, userID)
	}

	// Leave all relevant chats
	for _, c := range h.chats {
		c.Leave(userID)
	}
}

func (h *Hub) Dispatch(userID string, msg models.ClientMessage) {
	h.mu.RLock()
	c, ok := h.chats[msg.ChatID]
	h.mu.RUnlock()

	if !ok {
		// Chat doesn't exist
		return
	}

	// Validate if it is a DM, is the user part of it?
	if c.ID != "townhall" && !isUserInDM(userID, c.ID) {
		return
	}

	// Add record
	c.AddRecord(chat.ChatRecord{
		UserID:    userID,
		Content:   msg.Content,
		Timestamp: time.Now().Unix(),
	})
}

func (h *Hub) GetChats(userID string) []models.Chat {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var result []models.Chat

	for id, c := range h.chats {
		if id == "townhall" {
			result = append(result, models.Chat{
				ID:   c.ID,
				Name: "Town Hall",
			})
			continue
		}

		if isUserInDM(userID, id) {
			// Find other user name
			parts := strings.Split(id[3:], "_")
			otherID := parts[0]
			if otherID == userID {
				otherID = parts[1]
			}

			name := h.knownUsers[otherID].DisplayName
			if name == "" {
				name = "Unknown User"
			}

			result = append(result, models.Chat{
				ID:   c.ID,
				Name: name,
				IsDM: true,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		// Townhall always first.
		if result[i].Name == "Town Hall" {
			return true
		}
		if result[j].Name == "Town Hall" {
			return false
		}
		return result[i].Name < result[j].Name
	})

	return result
}

func (h *Hub) GetUsers() []models.User {
	h.mu.RLock()
	defer h.mu.RUnlock()

	users := make([]models.User, 0, len(h.knownUsers))
	for _, u := range h.knownUsers {
		users = append(users, u)
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].DisplayName < users[j].DisplayName
	})

	return users
}

func (h *Hub) GetUser(id string) (models.User, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	u, ok := h.knownUsers[id]
	return u, ok
}

func (h *Hub) handleRecordCallback(receiverID string, chatID string, record chat.ChatRecord) {
	h.mu.RLock()
	ch, online := h.connectedUsers[receiverID]
	h.mu.RUnlock()

	if !online {
		return
	}

	// Convert ChatRecord to models.Message (ServerMessage format)
	// models.Message.Timestamp is int64.
	msg := models.ServerMessage{
		Type:   models.ServerMessageTypeMessages,
		ChatID: chatID,
		Messages: []models.Message{
			{
				UserID:    record.UserID,
				Content:   record.Content,
				Timestamp: record.Timestamp,
			},
		},
	}

	select {
	case ch <- msg:
	default:
		// Drop message if channel full?
	}
}

// Helpers

func getDMID(u1, u2 string) string {
	ids := []string{u1, u2}
	sort.Strings(ids)
	return fmt.Sprintf("dm_%s_%s", ids[0], ids[1])
}

func isUserInDM(userID, chatID string) bool {
	if len(chatID) < 4 || chatID[:3] != "dm_" {
		return false
	}
	parts := strings.Split(chatID[3:], "_")
	if len(parts) != 2 {
		return false
	}
	return parts[0] == userID || parts[1] == userID
}
