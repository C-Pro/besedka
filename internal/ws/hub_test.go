package ws

import (
	"besedka/internal/models"
	"fmt"
	"testing"
	"time"
)

type MockUserProvider struct {
	users []models.User
}

func (m *MockUserProvider) GetUsers() ([]models.User, error) {
	return m.users, nil
}

func (m *MockUserProvider) GetUser(id string) (models.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return models.User{}, nil
}

type MockStorage struct {
	messages map[string][]models.Message
	chats    map[string]models.Chat
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		messages: make(map[string][]models.Message),
		chats:    make(map[string]models.Chat),
	}
}

func (m *MockStorage) UpsertMessage(msg models.Message) error {
	m.messages[msg.ChatID] = append(m.messages[msg.ChatID], msg)
	return nil
}

func (m *MockStorage) ListMessages(chatID string, from, to int64) ([]models.Message, error) {
	var results []models.Message
	if msgs, ok := m.messages[chatID]; ok {
		for _, r := range msgs {
			if r.Seq >= from && r.Seq <= to {
				results = append(results, r)
			}
		}
	}
	return results, nil
}

func (m *MockStorage) ListChats() ([]models.Chat, error) {
	var results []models.Chat
	for _, c := range m.chats {
		results = append(results, c)
	}
	return results, nil
}

func (m *MockStorage) UpsertChat(chat models.Chat) error {
	m.chats[chat.ID] = chat
	return nil
}

func TestHub_Lifecycle(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2},
	}
	store := NewMockStorage()
	h := NewHub(provider, store)

	// 1. Add Users - Automatically handled by NewHub via provider

	// Verify chats created
	// Townhall
	if _, ok := h.chats["townhall"]; !ok {
		t.Error("Townhall not created in memory")
	}
	if _, ok := store.chats["townhall"]; !ok {
		t.Error("Townhall not persisted")
	}

	// DM
	dmID := getDMID(user1.ID, user2.ID)
	if _, ok := h.chats[dmID]; !ok {
		t.Errorf("DM %s not created in memory", dmID)
	}
	if _, ok := store.chats[dmID]; !ok {
		t.Errorf("DM %s not persisted", dmID)
	}

	// 2. Join
	ch1 := h.Join(user1.ID)
	if ch1 == nil {
		t.Fatal("Join returned nil channel")
	}

	ch2 := h.Join(user2.ID)
	if ch2 == nil {
		t.Fatal("Join returned nil channel")
	}

	// 3. Dispatch & Receive (Townhall)
	msgContent := "hello townhall"
	h.Dispatch(user1.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  "townhall",
		Content: msgContent,
	})

	// Check receiving on user2
	select {
	case msg := <-ch2:
		if len(msg.Messages) == 0 {
			t.Fatal("Received empty message list")
		}
		if msg.Messages[0].Content != msgContent {
			t.Errorf("Expected content %s, got %s", msgContent, msg.Messages[0].Content)
		}
		if msg.ChatID != "townhall" {
			t.Errorf("Expected ChatID townhall, got %s", msg.ChatID)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for townhall message on ch2")
	}

	// Check receiving on user1 (sender also gets it via callback)
	select {
	case msg := <-ch1:
		if msg.Messages[0].Content != msgContent {
			t.Errorf("Sender did not receive own message")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for townhall message on ch1")
	}

	// 4. Dispatch & Receive (DM)
	dmContent := "secret"
	h.Dispatch(user2.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  dmID,
		Content: dmContent,
	})

	select {
	case msg := <-ch1:
		if msg.Messages[0].Content != dmContent {
			t.Errorf("User1 didn't get DM")
		}
		if msg.ChatID != dmID {
			t.Errorf("Wrong ChatID for DM")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for DM message")
	}

	// 5. Leave
	h.Leave(user1.ID)

	h.Dispatch(user2.ID, models.ClientMessage{
		ChatID:  dmID,
		Content: "are you there?",
	})

	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("Received message after leave")
		}
		// If !ok, it means channel is closed, which is correct for Leave()
	case <-time.After(100 * time.Millisecond):
		// Also OK if nothing received (though channel should be closed)
	}
}

func TestHub_JoinChat_ReturnsHistory(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2},
	}
	store := NewMockStorage()
	h := NewHub(provider, store)

	// User 1 connects
	ch1 := h.Join(user1.ID)
	if ch1 == nil {
		t.Fatal("Join returned nil channel")
	}

	// User 1 sends 5 messages
	for i := 0; i < 5; i++ {
		h.Dispatch(user1.ID, models.ClientMessage{
			Type:    models.ClientMessageTypeSend,
			ChatID:  "townhall",
			Content: fmt.Sprintf("msg %d", i),
		})
		// Consume own message to empty channel
		<-ch1
	}

	// User 2 connects
	ch2 := h.Join(user2.ID)

	// User 2 sends Join command for Townhall
	h.Dispatch(user2.ID, models.ClientMessage{
		Type:   models.ClientMessageTypeJoin,
		ChatID: "townhall",
	})

	// User 2 should receive history
	select {
	case msg := <-ch2:
		if msg.Type != models.ServerMessageTypeMessages {
			t.Errorf("Expected message type 'messages', got %s", msg.Type)
		}
		if len(msg.Messages) != 5 {
			t.Errorf("Expected 5 messages, got %d", len(msg.Messages))
		}
		// Verify order and sequence
		for i, m := range msg.Messages {
			expected := fmt.Sprintf("msg %d", i)
			if m.Content != expected {
				t.Errorf("Message %d: expected content %q, got %q", i, expected, m.Content)
			}
			// Chat sequences start at 0 (or 1 depending on implementation, let's just check it increases)
			// Actually chat.go initializes LastSeq to -1, first add is 0.
			if m.Seq != int64(i+1) {
				t.Errorf("Message %d: expected seq %d, got %d", i, i+1, m.Seq)
			}
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for history")
	}
}
