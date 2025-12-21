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
	knownUsers map[string]bool

	mu sync.RWMutex
}

func NewHub() *Hub {
	h := &Hub{
		chats:          make(map[string]*chat.Chat),
		connectedUsers: make(map[string]chan models.ServerMessage),
		knownUsers:     make(map[string]bool),
	}

	// Create Townhall
	h.createChat("townhall", 100)

	// Populate users from stubs
	for _, u := range stubs.Users {
		h.AddUser(u.ID)
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

func (h *Hub) AddUser(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.knownUsers[userID] {
		return
	}
	h.knownUsers[userID] = true

	// Add to Townhall
	if ch, ok := h.chats["townhall"]; ok {
		// We don't strictly need to "add" them to internal list if it's open,
		// but chat.go uses Members map to track online status.
		// We'll treat all known users as potentially "offline" members initially?
		// Actually chat.go only tracks members for dispatching.
		// We need to ensure that when a user Joins, they join the chat.
		// For now, just ensuring the chat exists is enough.
		_ = ch
	}

	// Create DMs with all other existing users
	for otherID := range h.knownUsers {
		if otherID == userID {
			continue
		}
		// Create deterministic ID for DM
		dmID := getDMID(userID, otherID)
		if _, exists := h.chats[dmID]; !exists {
			h.createChat(dmID, 50) // DM limit 50?
		}
	}
}

func (h *Hub) Join(userID string) chan models.ServerMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

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
