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
	// User1 might have received "User2 online" message first
	timeout := time.After(1 * time.Second)
	var foundMessage bool
	for !foundMessage {
		select {
		case msg := <-ch1:
			if msg.Type == models.ServerMessageTypeOnline {
				continue
			}
			if msg.Type == models.ServerMessageTypeMessages {
				if len(msg.Messages) > 0 && msg.Messages[0].Content == msgContent {
					foundMessage = true
				}
			}
		case <-timeout:
			t.Error("Timeout waiting for townhall message on ch1")
			foundMessage = true // Break loop
		}
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

func TestHub_Broadcasting(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}
	user3 := models.User{ID: "u3", DisplayName: "User 3"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2, user3},
	}
	store := NewMockStorage()
	h := NewHub(provider, store)

	ch1 := h.Join(user1.ID)
	ch2 := h.Join(user2.ID)

	// Consume initial online messages
	// User 1 receives User 2 online
	select {
	case msg := <-ch1:
		if msg.Type != models.ServerMessageTypeOnline || msg.UserID != user2.ID {
			t.Errorf("User 1 expected User 2 online, got %v", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for online message")
	}

	// Test BroadcastNewUser
	h.BroadcastNewUser(user3)

	// User 1 should receive New User message
	select {
	case msg := <-ch1:
		if msg.Type != models.ServerMessageTypeNew {
			t.Errorf("Expected New User message, got %s", msg.Type)
		}
		if msg.User.ID != user3.ID {
			t.Errorf("Expected user u3, got %s", msg.User.ID)
		}
		// Check that a Chat object is included (DM with u3)
		if msg.Chat.ID == "" || !msg.Chat.IsDM {
			t.Errorf("Expected DM chat in New User message, got %v", msg.Chat)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for New User message on ch1")
	}

	// User 2 should also receive it
	select {
	case msg := <-ch2:
		if msg.Type != models.ServerMessageTypeNew {
			t.Errorf("Expected New User message, got %s", msg.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for New User message on ch2")
	}

	// Test Leave (Offline)
	h.Leave(user2.ID)

	// User 1 should receive Offline message
	select {
	case msg := <-ch1:
		if msg.Type != models.ServerMessageTypeOffline {
			t.Errorf("Expected Offline message, got %s", msg.Type)
		}
		if msg.UserID != user2.ID {
			t.Errorf("Expected user u2 offline, got %s", msg.UserID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for Offline message on ch1")
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

	// Consume potential "User 2 online" message on ch1 (not ch2, sender doesn't get online msg for self)
	// ch2 just joined, it receives nothing yet.

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

func TestHub_RemoveDeletedUser(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}
	user3 := models.User{ID: "u3", DisplayName: "User 3"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2, user3},
	}
	store := NewMockStorage()
	h := NewHub(provider, store)

	// Connect all users
	ch1 := h.Join(user1.ID)
	ch2 := h.Join(user2.ID)
	ch3 := h.Join(user3.ID)

	// Drain online messages from all channels
	for i := 0; i < 2; i++ {
		select {
		case <-ch1:
		case <-time.After(100 * time.Millisecond):
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ch2:
		case <-time.After(100 * time.Millisecond):
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ch3:
		case <-time.After(100 * time.Millisecond):
		}
	}

	// Verify DM chats exist before deletion
	dmID12 := getDMID(user1.ID, user2.ID)
	dmID13 := getDMID(user1.ID, user3.ID)
	dmID23 := getDMID(user2.ID, user3.ID)

	h.mu.RLock()
	if _, ok := h.chats[dmID12]; !ok {
		t.Error("DM between u1 and u2 should exist")
	}
	if _, ok := h.chats[dmID13]; !ok {
		t.Error("DM between u1 and u3 should exist")
	}
	if _, ok := h.chats[dmID23]; !ok {
		t.Error("DM between u2 and u3 should exist")
	}
	if _, ok := h.connectedUsers[user1.ID]; !ok {
		t.Error("User 1 should be connected")
	}
	h.mu.RUnlock()

	// Remove user1
	h.RemoveDeletedUser(user1.ID)

	// Verify user1's connection is closed
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("User 1's channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected user 1's channel to be closed")
	}

	// Verify user1 is removed from connectedUsers
	h.mu.RLock()
	if _, ok := h.connectedUsers[user1.ID]; ok {
		t.Error("User 1 should be removed from connectedUsers")
	}

	// Verify DM chats involving user1 are removed
	if _, ok := h.chats[dmID12]; ok {
		t.Error("DM between u1 and u2 should be removed")
	}
	if _, ok := h.chats[dmID13]; ok {
		t.Error("DM between u1 and u3 should be removed")
	}

	// Verify DM chat not involving user1 still exists
	if _, ok := h.chats[dmID23]; !ok {
		t.Error("DM between u2 and u3 should still exist")
	}

	// Verify townhall still exists
	if _, ok := h.chats["townhall"]; !ok {
		t.Error("Townhall should still exist")
	}
	h.mu.RUnlock()

	// Verify deletion event is broadcast to other connected users
	select {
	case msg := <-ch2:
		if msg.Type != models.ServerMessageTypeDeleted {
			t.Errorf("Expected Deleted message, got %s", msg.Type)
		}
		if msg.UserID != user1.ID {
			t.Errorf("Expected deleted user ID %s, got %s", user1.ID, msg.UserID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for deletion message on ch2")
	}

	select {
	case msg := <-ch3:
		if msg.Type != models.ServerMessageTypeDeleted {
			t.Errorf("Expected Deleted message, got %s", msg.Type)
		}
		if msg.UserID != user1.ID {
			t.Errorf("Expected deleted user ID %s, got %s", user1.ID, msg.UserID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for deletion message on ch3")
	}
}
