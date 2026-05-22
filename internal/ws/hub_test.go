package ws

import (
	"besedka/internal/models"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

const testTimeout = 100 * time.Millisecond

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
	lastSeen []models.LastSeenEntry
	mu       sync.Mutex
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

func (m *MockStorage) SaveLastSeenBatch(batch []models.LastSeenEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range batch {
		found := false
		for i, existing := range m.lastSeen {
			if existing.UserID == entry.UserID && existing.ChatID == entry.ChatID {
				m.lastSeen[i].Seq = entry.Seq
				found = true
				break
			}
		}
		if !found {
			m.lastSeen = append(m.lastSeen, entry)
		}
	}
	return nil
}

func (m *MockStorage) ListLastSeen() ([]models.LastSeenEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]models.LastSeenEntry, len(m.lastSeen))
	copy(copied, m.lastSeen)
	return copied, nil
}

type MockPushService struct{}

func (m *MockPushService) SendNotification(userID string, payload []byte) error {
	return nil
}

// drainMessages consumes up to count messages from a channel during test setup.
// This is used to clear expected messages (like online notifications) that would
// otherwise interfere with testing subsequent behavior. If fewer than count messages
// are available, the function will wait testTimeout per remaining message before continuing.
func drainMessages(ch <-chan models.ServerMessage, count int) {
	for i := 0; i < count; i++ {
		select {
		case <-ch:
		case <-time.After(testTimeout):
		}
	}
}

func TestHub_MultipleConnections(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	provider := &MockUserProvider{
		users: []models.User{user1},
	}
	store := NewMockStorage()
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	// Two connections for the same user
	ch1 := h.Join(user1.ID)
	ch2 := h.Join(user1.ID)

	// Dispatch message to townhall
	msgContent := "hello everyone"
	h.Dispatch(user1.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  "townhall",
		Content: msgContent,
	})

	expectedHTML := fmt.Sprintf("<p>%s</p>\n", msgContent)

	// Check receiving on ch1
	select {
	case msg := <-ch1:
		if msg.Type == models.ServerMessageTypeOnline {
			// Skip online notification, try again
			select {
			case msg = <-ch1:
			case <-time.After(testTimeout):
				t.Error("Timeout waiting for message on ch1 after online notification")
			}
		}
		if len(msg.Messages) == 0 || msg.Messages[0].Content != expectedHTML {
			t.Errorf("ch1: expected content %s, got %+v", expectedHTML, msg.Messages)
		}
	case <-time.After(testTimeout):
		t.Error("Timeout waiting for message on ch1")
	}

	// Check receiving on ch2
	select {
	case msg := <-ch2:
		if msg.Type == models.ServerMessageTypeOnline {
			// Skip online notification, try again
			select {
			case msg = <-ch2:
			case <-time.After(testTimeout):
				t.Error("Timeout waiting for message on ch2 after online notification")
			}
		}
		if len(msg.Messages) == 0 || msg.Messages[0].Content != expectedHTML {
			t.Errorf("ch2: expected content %s, got %+v", expectedHTML, msg.Messages)
		}
	case <-time.After(testTimeout):
		t.Error("Timeout waiting for message on ch2")
	}
}

func TestHub_Lifecycle(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2},
	}
	store := NewMockStorage()
	h := NewHub(context.Background(), provider, store, &MockPushService{})

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
		expectedHTML := fmt.Sprintf("<p>%s</p>\n", msgContent)
		if msg.Messages[0].Content != expectedHTML {
			t.Errorf("Expected content %s, got %s", expectedHTML, msg.Messages[0].Content)
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
				expectedHTML := fmt.Sprintf("<p>%s</p>\n", msgContent)
				if len(msg.Messages) > 0 && msg.Messages[0].Content == expectedHTML {
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
		expectedHTML := fmt.Sprintf("<p>%s</p>\n", dmContent)
		if msg.Messages[0].Content != expectedHTML {
			t.Errorf("User1 didn't get DM")
		}
		if msg.ChatID != dmID {
			t.Errorf("Wrong ChatID for DM")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for DM message")
	}

	// 5. Leave
	h.Leave(user1.ID, nil)

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
	case <-time.After(testTimeout):
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
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	ch1 := h.Join(user1.ID)
	ch2 := h.Join(user2.ID)

	// Consume initial online messages
	// User 1 receives User 2 online
	select {
	case msg := <-ch1:
		if msg.Type != models.ServerMessageTypeOnline || msg.UserID != user2.ID {
			t.Errorf("User 1 expected User 2 online, got %v", msg)
		}
	case <-time.After(testTimeout):
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
	case <-time.After(testTimeout):
		t.Error("Timeout waiting for New User message on ch1")
	}

	// User 2 should also receive it
	select {
	case msg := <-ch2:
		if msg.Type != models.ServerMessageTypeNew {
			t.Errorf("Expected New User message, got %s", msg.Type)
		}
	case <-time.After(testTimeout):
		t.Error("Timeout waiting for New User message on ch2")
	}

	// Test Leave (Offline)
	h.Leave(user2.ID, nil)

	// User 1 should receive Offline message
	select {
	case msg := <-ch1:
		if msg.Type != models.ServerMessageTypeOffline {
			t.Errorf("Expected Offline message, got %s", msg.Type)
		}
		if msg.UserID != user2.ID {
			t.Errorf("Expected user u2 offline, got %s", msg.UserID)
		}
	case <-time.After(testTimeout):
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
	h := NewHub(context.Background(), provider, store, &MockPushService{})

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
			expected := fmt.Sprintf("<p>msg %d</p>\n", i)
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
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	// Connect all users
	ch1 := h.Join(user1.ID)
	ch2 := h.Join(user2.ID)
	ch3 := h.Join(user3.ID)

	// Drain online messages from all channels
	// Each user receives online notifications for the other 2 users joining
	drainMessages(ch1, 2)
	drainMessages(ch2, 2)
	drainMessages(ch3, 2)

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
	case <-time.After(testTimeout):
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
	case <-time.After(testTimeout):
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
	case <-time.After(testTimeout):
		t.Error("Timeout waiting for deletion message on ch3")
	}
}

func TestHub_GetChats_TownHallAvatarURL(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2},
	}
	store := NewMockStorage()
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	chats := h.GetChats(user1.ID)

	var townhall *models.Chat
	var dms []models.Chat
	for i := range chats {
		if chats[i].ID == "townhall" {
			townhall = &chats[i]
		} else if chats[i].IsDM {
			dms = append(dms, chats[i])
		}
	}

	if townhall != nil {
		if townhall.AvatarURL != "/besedka.png" {
			t.Errorf("Town Hall AvatarURL: expected %q, got %q", "/besedka.png", townhall.AvatarURL)
		}
		if townhall.Name != "Town Hall" {
			t.Errorf("Town Hall Name: expected %q, got %q", "Town Hall", townhall.Name)
		}
	} else {
		t.Fatal("Town Hall chat not found in GetChats result")
	}

	for _, dm := range dms {
		if dm.AvatarURL != "" {
			t.Errorf("DM chat %q should not have AvatarURL, got %q", dm.ID, dm.AvatarURL)
		}
	}
}

func TestHub_FetchMessages_ReturnsRange(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}

	provider := &MockUserProvider{
		users: []models.User{user1},
	}
	store := NewMockStorage()
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	ch1 := h.Join(user1.ID)
	if ch1 == nil {
		t.Fatal("Join returned nil channel")
	}

	// User 1 sends 10 messages
	for i := 0; i < 10; i++ {
		h.Dispatch(user1.ID, models.ClientMessage{
			Type:    models.ClientMessageTypeSend,
			ChatID:  "townhall",
			Content: fmt.Sprintf("msg %d", i),
		})
		<-ch1 // Consume own message
	}

	// Fetch messages from sequence 3 to 6
	h.Dispatch(user1.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeFetch,
		ChatID:  "townhall",
		FromSeq: 3,
		ToSeq:   6,
	})

	timeout := time.After(1 * time.Second)
	var found bool
	for !found {
		select {
		case msg := <-ch1:
			if msg.Type == models.ServerMessageTypeMessages {
				if len(msg.Messages) != 4 {
					t.Errorf("Expected 4 messages, got %d", len(msg.Messages))
				}
				// Verify sequences (3, 4, 5, 6)
				for i, m := range msg.Messages {
					expectedSeq := int64(3 + i)
					if m.Seq != expectedSeq {
						t.Errorf("Message %d: expected seq %d, got %d", i, expectedSeq, m.Seq)
					}
					expectedContent := fmt.Sprintf("<p>msg %d</p>\n", expectedSeq-1)
					if m.Content != expectedContent {
						t.Errorf("Message %d: expected content %q, got %q", i, expectedContent, m.Content)
					}
				}
				found = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for fetched messages")
		}
	}
}

func TestHub_EnsureDMsFor_JoinsConnectedUsers(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}
	userNew := models.User{ID: "unew", DisplayName: "New User"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2},
	}
	store := NewMockStorage()
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	// Connect user1
	ch1 := h.Join(user1.ID)

	// User1 might receive online notifications
	drainMessages(ch1, 1)

	// Now a new user registers.
	h.EnsureDMsFor(userNew, []models.User{user1, user2, userNew})

	// Dispatch a message from the new user to user1
	dmID := getDMID(userNew.ID, user1.ID)
	h.Dispatch(userNew.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  dmID,
		Content: "hello from new user",
	})

	// User 1 should receive it because EnsureDMsFor added them to the chat
	timeout := time.After(testTimeout)
	var found bool
	for !found {
		select {
		case msg := <-ch1:
			if msg.Type == models.ServerMessageTypeMessages {
				if len(msg.Messages) > 0 && msg.Messages[0].Content == "<p>hello from new user</p>\n" {
					found = true
				}
			}
		case <-timeout:
			t.Fatal("Timeout waiting for message on new DM chat, user1 was likely not joined")
		}
	}
}

func TestHub_Leave_ReplacedConnection(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2},
	}
	store := NewMockStorage()
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	// User 1 connects (first connection)
	ch1a := h.Join(user1.ID)
	ch2 := h.Join(user2.ID)

	// Drain initial online notifications
	drainMessages(ch1a, 1) // u2 online
	drainMessages(ch2, 1)  // u1 online

	// User 1 reconnects: a new connection replaces the old one
	ch1b := h.Join(user1.ID)

	// Drain the new online notification for u1 that u2 receives
	drainMessages(ch2, 1) // u1 online again

	// At this point ch1a is NOT yet closed — Join does not close the old channel.
	// It is only closed when the old WebSocket handler calls Leave with the stale channel.
	h.Leave(user1.ID, ch1a)

	// After Leave with the stale channel, ch1a should now be closed.
	select {
	case _, ok := <-ch1a:
		if ok {
			t.Error("Old channel should be closed after Leave with stale channel")
		}
	case <-time.After(testTimeout):
		t.Error("Expected old channel to be closed after Leave with stale channel")
	}

	// User 2 must NOT receive any notification (especially not offline) for user 1
	select {
	case msg := <-ch2:
		t.Errorf("Unexpected message received by user 2 after stale Leave: type=%s userID=%s", msg.Type, msg.UserID)
	case <-time.After(testTimeout):
		// No messages received - correct behavior
	}

	// The new connection ch1b must still be registered in connectedUsers
	h.mu.RLock()
	registeredCh, ok := h.connectedUsers[user1.ID]
	h.mu.RUnlock()

	if !ok {
		t.Error("User 1 should still be in connectedUsers after stale Leave")
	}
	if len(registeredCh) != 1 || registeredCh[0] != ch1b {
		t.Error("connectedUsers should hold only the new channel after stale Leave")
	}

	// Verify new connection still works: user 1 can still receive messages
	h.Dispatch(user2.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  "townhall",
		Content: "still there?",
	})

	select {
	case msg := <-ch1b:
		if msg.Type == models.ServerMessageTypeMessages && len(msg.Messages) > 0 {
			// Got the message - new connection is intact
		} else {
			t.Errorf("Unexpected message type on ch1b: %s", msg.Type)
		}
	case <-time.After(testTimeout):
		t.Error("User 1's new connection should still receive messages after stale Leave")
	}
}

func TestHub_LocationBroadcast(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}

	provider := &MockUserProvider{
		users: []models.User{user1, user2},
	}
	store := NewMockStorage()
	h := NewHub(context.Background(), provider, store, &MockPushService{})

	ch1 := h.Join(user1.ID)
	ch2 := h.Join(user2.ID)

	drainMessages(ch1, 1) // u2 online
	drainMessages(ch2, 1) // u1 online (from ch2's perspective, u1 was already online)

	// User 1 sends location
	loc := models.Location{Lat: 37.7749, Lng: -122.4194}
	h.Dispatch(user1.ID, models.ClientMessage{
		Type:     models.ClientMessageTypeLocation,
		Location: &loc,
	})

	// User 1 should also receive its own location broadcast
	timeout := time.After(time.Second)
	found := false
	for !found {
		select {
		case msg := <-ch1:
			if msg.Type == models.ServerMessageTypeLocation {
				if len(msg.UserLocations) != 1 {
					t.Fatalf("Expected 1 user location for sender, got %d", len(msg.UserLocations))
				}
				ul := msg.UserLocations[0]
				if ul.UserID != user1.ID {
					t.Errorf("Expected user ID %s, got %s", user1.ID, ul.UserID)
				}
				found = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for own location broadcast")
		}
	}

	// User 2 should receive location broadcast
	timeout = time.After(time.Second)
	found = false
	for !found {
		select {
		case msg := <-ch2:
			if msg.Type == models.ServerMessageTypeLocation {
				if len(msg.UserLocations) != 1 {
					t.Fatalf("Expected 1 user location for other, got %d", len(msg.UserLocations))
				}
				ul := msg.UserLocations[0]
				if ul.UserID != user1.ID {
					t.Errorf("Expected user ID %s for other, got %s", user1.ID, ul.UserID)
				}
				if ul.Location.Lat != loc.Lat || ul.Location.Lng != loc.Lng {
					t.Errorf("Location mismatch: got %+v", ul.Location)
				}
				found = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for location broadcast on ch2")
		}
	}

	// Verify location is cached
	cachedLoc, err := h.userLocations.Get(user1.ID)
	if err != nil {
		t.Fatalf("Location not cached: %v", err)
	}
	if cachedLoc.Lat != loc.Lat || cachedLoc.Lng != loc.Lng {
		t.Errorf("Cached location mismatch: got %+v", cachedLoc)
	}

	// New user joins and should receive bulk locations
	user3 := models.User{ID: "u3", DisplayName: "User 3"}
	provider.users = append(provider.users, user3)
	h.EnsureDMsFor(user3, provider.users)
	ch3 := h.Join(user3.ID)

	timeout = time.After(time.Second)
	var gotLocations bool
	for !gotLocations {
		select {
		case msg := <-ch3:
			if msg.Type == models.ServerMessageTypeLocation {
				if len(msg.UserLocations) < 1 {
					t.Fatalf("Expected at least 1 user location in bulk, got %d", len(msg.UserLocations))
				}
				gotLocations = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for bulk location on new user join")
		}
	}
}

func TestHub_ReadReceipts(t *testing.T) {
	provider := &MockUserProvider{
		users: []models.User{
			{ID: "u1", DisplayName: "User 1"},
			{ID: "u2", DisplayName: "User 2"},
		},
	}
	store := NewMockStorage()

	// 1. Setup pre-existing read receipt in database
	preLastSeen := []models.LastSeenEntry{
		{UserID: "u1", ChatID: "townhall", Seq: 5},
	}
	_ = store.SaveLastSeenBatch(preLastSeen)

	// Setup townhall chat in store
	_ = store.UpsertChat(models.Chat{
		ID:      "townhall",
		Name:    "Town Hall",
		LastSeq: 10,
	})

	h := NewHub(context.Background(), provider, store, &MockPushService{})

	// Verify loaded last seen
	h.mu.RLock()
	val, exists := h.lastSeenSeq[userChatKey{UserID: "u1", ChatID: "townhall"}]
	h.mu.RUnlock()
	if !exists || val != 5 {
		t.Fatalf("expected last seen seq to be loaded as 5, got: exists=%v, val=%d", exists, val)
	}

	// 2. Test GetChats returning LastSeenSeq
	chats := h.GetChats("u1")
	var townhallChat *models.Chat
	for i := range chats {
		if chats[i].ID == "townhall" {
			townhallChat = &chats[i]
		}
	}
	if townhallChat == nil {
		t.Fatal("townhall chat not found in GetChats")
	}
	if townhallChat.LastSeenSeq != 5 {
		t.Errorf("expected LastSeenSeq 5, got %d", townhallChat.LastSeenSeq)
	}

	// Verify default to LastSeq when no last seen exists (e.g. for User 2)
	chats2 := h.GetChats("u2")
	var townhallChat2 *models.Chat
	for i := range chats2 {
		if chats2[i].ID == "townhall" {
			townhallChat2 = &chats2[i]
		}
	}
	if townhallChat2 == nil {
		t.Fatal("townhall chat not found in GetChats for u2")
	}
	if townhallChat2.LastSeenSeq != 10 {
		t.Errorf("expected LastSeenSeq to default to LastSeq 10 for u2, got %d", townhallChat2.LastSeenSeq)
	}

	// 3. Test progress of last seen via Dispatch
	h.Dispatch("u1", models.ClientMessage{
		Type:   models.ClientMessageTypeRead,
		ChatID: "townhall",
		Seq:    8,
	})

	h.mu.RLock()
	val, exists = h.lastSeenSeq[userChatKey{UserID: "u1", ChatID: "townhall"}]
	h.mu.RUnlock()
	if !exists || val != 8 {
		t.Fatalf("expected progressed last seen seq to be 8, got %d", val)
	}

	// Test that we cannot regress it
	h.Dispatch("u1", models.ClientMessage{
		Type:   models.ClientMessageTypeRead,
		ChatID: "townhall",
		Seq:    4,
	})

	h.mu.RLock()
	val, exists = h.lastSeenSeq[userChatKey{UserID: "u1", ChatID: "townhall"}]
	h.mu.RUnlock()
	if !exists || val != 8 {
		t.Fatalf("expected last seen seq to stay 8, got %d", val)
	}

	// 4. Test database write on flush
	h.flushLastSeen()

	dbList, err := store.ListLastSeen()
	if err != nil {
		t.Fatalf("ListLastSeen failed: %v", err)
	}

	var updatedEntry *models.LastSeenEntry
	for i := range dbList {
		if dbList[i].UserID == "u1" && dbList[i].ChatID == "townhall" {
			updatedEntry = &dbList[i]
		}
	}
	if updatedEntry == nil || updatedEntry.Seq != 8 {
		t.Errorf("expected database to have been updated to seq 8 on flush, got: %+v", updatedEntry)
	}
}
