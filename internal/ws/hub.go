package ws

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"besedka/internal/chat"
	"besedka/internal/models"
)

// Number of records chats will keep in memory.
const chatMaxRecords = 500

type Hub struct {
	// Map of chatID -> Chat object
	chats map[string]*chat.Chat

	// Map of userID -> Connection channel
	connectedUsers map[string]chan models.ServerMessage

	userProvider userProvider
	storage      storage

	mu sync.RWMutex
}

type storage interface {
	UpsertMessage(message models.Message) error
	ListMessages(chatID string, from, to int64) ([]models.Message, error)
	ListChats() ([]models.Chat, error)
	UpsertChat(chat models.Chat) error
}

type userProvider interface {
	GetUser(id string) (models.User, error)
	GetUsers() ([]models.User, error)
}

func NewHub(userProvider userProvider, storage storage) *Hub {
	h := &Hub{
		chats:          make(map[string]*chat.Chat),
		connectedUsers: make(map[string]chan models.ServerMessage),
		userProvider:   userProvider,
		storage:        storage,
	}

	chats, err := storage.ListChats()
	if err != nil {
		slog.Error("failed to list chats from storage", "error", err)
	} else {
		for _, c := range chats {
			h.restoreChat(c)
		}
	}

	// Ensure Townhall exists
	if _, ok := h.chats["townhall"]; !ok {
		h.createChat("townhall", chatMaxRecords, false)
	}

	// Build initial DMs based on existing users using userProvider.
	// This usually does nothing as EnsureDMsFor is idempotent and chats are restored from storage.
	users, _ := userProvider.GetUsers()
	for _, u := range users {
		h.EnsureDMsFor(u, users)
	}

	return h
}

func (h *Hub) restoreChat(modelChat models.Chat) {
	c := chat.New(chat.Config{
		ID:             modelChat.ID,
		MaxRecords:     chatMaxRecords,
		RecordCallback: h.handleRecordCallback,
		Storage:        h.storage,
	})

	if modelChat.LastSeq > 0 {
		from := int64(modelChat.LastSeq) - chatMaxRecords + 1
		if from < 1 {
			from = 1
		}
		to := int64(modelChat.LastSeq)

		msgs, err := h.storage.ListMessages(c.ID, from, to)
		if err != nil {
			slog.Error("failed to restore messages", "chatID", c.ID, "error", err)
		} else {
			for _, m := range msgs {
				rec := chat.ChatRecord{
					Seq:         chat.Seq(m.Seq),
					Timestamp:   m.Timestamp,
					UserID:      m.UserID,
					Content:     m.Content,
					Attachments: m.Attachments,
				}
				c.Records = append(c.Records, rec)
				if c.FirstSeq == 0 {
					c.FirstSeq = rec.Seq
				}
				c.LastSeq = rec.Seq
			}
			c.LastIndex = len(c.Records) - 1
		}
	}

	h.chats[c.ID] = c
}

func (h *Hub) createChat(id string, maxRecords int, isDM bool) *chat.Chat {
	c := chat.New(chat.Config{
		ID:             id,
		MaxRecords:     maxRecords,
		RecordCallback: h.handleRecordCallback,
		Storage:        h.storage,
	})

	if err := h.storage.UpsertChat(models.Chat{
		ID:   id,
		Name: id,
		IsDM: isDM,
	}); err != nil {
		slog.Error("failed to upsert chat", "chatID", id, "error", err)
	}

	h.chats[id] = c
	return c
}

func (h *Hub) EnsureDMsFor(user models.User, allUsers []models.User) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Create DMs with all other existing users
	for _, other := range allUsers {
		if other.ID == user.ID {
			continue
		}
		// Create deterministic ID for DM
		dmID := getDMID(user.ID, other.ID)
		if _, exists := h.chats[dmID]; !exists {
			h.createChat(dmID, chatMaxRecords, true)
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

	// Handle message based on type
	switch msg.Type {
	case models.ClientMessageTypeSend:
		if err := c.AddRecord(chat.ChatRecord{
			UserID:      userID,
			Content:     msg.Content,
			Attachments: msg.Attachments,
			Timestamp:   time.Now().Unix(),
		}); err != nil {
			slog.Error("failed to add record", "chatID", c.ID, "userID", userID, "error", err)
		}
	case models.ClientMessageTypeJoin:
		// Send last 100 messages
		records, err := c.GetLastRecords(100)
		if err != nil {
			slog.Error("failed to get last records", "error", err)
			return
		}

		messages := make([]models.Message, len(records))
		for i, r := range records {
			messages[i] = models.Message{
				Seq:         int64(r.Seq),
				UserID:      r.UserID,
				Content:     r.Content,
				Timestamp:   r.Timestamp,
				Attachments: r.Attachments,
			}
		}

		serverMsg := models.ServerMessage{
			Type:     models.ServerMessageTypeMessages,
			ChatID:   c.ID,
			Messages: messages,
		}

		h.sendToUser(userID, serverMsg)
	}
}

func (h *Hub) sendToUser(userID string, msg models.ServerMessage) {
	h.mu.RLock()
	ch, online := h.connectedUsers[userID]
	h.mu.RUnlock()

	if !online {
		return
	}

	select {
	case ch <- msg:
	default:
		slog.Warn("Message channel full, dropping message", "userID", userID)
	}
}

func (h *Hub) GetChats(userID string) []models.Chat {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var result []models.Chat

	for id, c := range h.chats {
		if id == "townhall" {
			result = append(result, models.Chat{
				ID:      c.ID,
				Name:    "Town Hall",
				LastSeq: int(c.LastSeq),
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

			u, err := h.userProvider.GetUser(otherID)
			name := "Unknown User"
			if err == nil {
				name = u.DisplayName
			}
			if name == "" {
				name = "Unknown User"
			}

			_, online := h.connectedUsers[otherID]
			result = append(result, models.Chat{
				ID:     c.ID,
				Name:   name,
				IsDM:   true,
				Online: online,
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
		// Everything else is sorted alphabetically.
		// TODO: order by last message time.
		// TODO: support pinned (favorite) chats.
		return result[i].Name < result[j].Name
	})

	return result
}

func (h *Hub) GetUser(userID string) (models.User, error) {
	return h.userProvider.GetUser(userID)
}

func (h *Hub) handleRecordCallback(receiverID string, chatID string, record chat.ChatRecord) {
	h.mu.RLock()
	ch, online := h.connectedUsers[receiverID]
	h.mu.RUnlock()

	if !online {
		return
	}

	msg := models.ServerMessage{
		Type:   models.ServerMessageTypeMessages,
		ChatID: chatID,
		Messages: []models.Message{
			{
				Seq:         int64(record.Seq),
				UserID:      record.UserID,
				Content:     record.Content,
				Timestamp:   record.Timestamp,
				Attachments: record.Attachments,
			},
		},
	}

	select {
	case ch <- msg:
	default:
		// TODO: disconnect user? Channel has size 100 so if the client
		// is 100 messages behind, there's something off with it.
		slog.Warn("Message channel full, dropping message", "chatID", chatID, "userID", receiverID)
	}
}

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
