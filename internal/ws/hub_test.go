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

// expectMessages blocks until a ServerMessageTypeMessages is received for the specified chatID
// or the timeout is reached. It discards other server messages (e.g. read receipts, online notifications).
func expectMessages(t *testing.T, ch <-chan models.ServerMessage, chatID string) models.ServerMessage {
	t.Helper()
	timeout := time.After(1 * time.Second)
	for {
		select {
		case msg := <-ch:
			if msg.Type == models.ServerMessageTypeMessages && msg.ChatID == chatID {
				return msg
			}
		case <-timeout:
			t.Fatalf("timeout waiting for messages in chat %s", chatID)
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
	}, ch1)

	expectedHTML := fmt.Sprintf("<p>%s</p>\n", msgContent)

	// Check receiving on ch1
	msg1 := expectMessages(t, ch1, "townhall")
	if len(msg1.Messages) == 0 || msg1.Messages[0].Content != expectedHTML {
		t.Errorf("ch1: expected content %s, got %+v", expectedHTML, msg1.Messages)
	}

	// Check receiving on ch2
	msg2 := expectMessages(t, ch2, "townhall")
	if len(msg2.Messages) == 0 || msg2.Messages[0].Content != expectedHTML {
		t.Errorf("ch2: expected content %s, got %+v", expectedHTML, msg2.Messages)
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
	}, nil)

	// Check receiving on user2
	msg2 := expectMessages(t, ch2, "townhall")
	if len(msg2.Messages) == 0 {
		t.Fatal("Received empty message list")
	}
	expectedHTML := fmt.Sprintf("<p>%s</p>\n", msgContent)
	if msg2.Messages[0].Content != expectedHTML {
		t.Errorf("Expected content %s, got %s", expectedHTML, msg2.Messages[0].Content)
	}

	// Check receiving on user1 (sender also gets it via callback)
	msg1 := expectMessages(t, ch1, "townhall")
	if len(msg1.Messages) == 0 {
		t.Fatal("Received empty message list")
	}
	if msg1.Messages[0].Content != expectedHTML {
		t.Errorf("Expected content %s, got %s", expectedHTML, msg1.Messages[0].Content)
	}

	// 4. Dispatch & Receive (DM)
	dmContent := "secret"
	h.Dispatch(user2.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  dmID,
		Content: dmContent,
	}, nil)

	dmMsg := expectMessages(t, ch1, dmID)
	if len(dmMsg.Messages) == 0 {
		t.Fatal("Received empty message list")
	}
	expectedDMHTML := fmt.Sprintf("<p>%s</p>\n", dmContent)
	if dmMsg.Messages[0].Content != expectedDMHTML {
		t.Errorf("User1 didn't get DM")
	}

	// 5. Leave
	h.Leave(user1.ID, nil)

	h.Dispatch(user2.ID, models.ClientMessage{
		ChatID:  dmID,
		Content: "are you there?",
	}, nil)

	timeout := time.After(testTimeout)
	for {
		select {
		case msg, ok := <-ch1:
			if !ok {
				// Channel is closed, which is correct
				return
			}
			if msg.Type == models.ServerMessageTypeMessages {
				t.Errorf("Received message after leave: %+v", msg)
				return
			}
		case <-timeout:
			// Timeout is fine, though channel should ideally be closed
			return
		}
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
		}, ch1)
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
	}, nil)

	// User 2 should receive history
	msg := expectMessages(t, ch2, "townhall")
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
		}, ch1)
		<-ch1 // Consume own message
	}

	// Fetch messages from sequence 3 to 6
	h.Dispatch(user1.ID, models.ClientMessage{
		Type:    models.ClientMessageTypeFetch,
		ChatID:  "townhall",
		FromSeq: 3,
		ToSeq:   6,
	}, nil)

	msgFetch := expectMessages(t, ch1, "townhall")
	if len(msgFetch.Messages) != 4 {
		t.Errorf("Expected 4 messages, got %d", len(msgFetch.Messages))
	}
	// Verify sequences (3, 4, 5, 6)
	for i, m := range msgFetch.Messages {
		expectedSeq := int64(3 + i)
		if m.Seq != expectedSeq {
			t.Errorf("Message %d: expected seq %d, got %d", i, expectedSeq, m.Seq)
		}
		expectedContent := fmt.Sprintf("<p>msg %d</p>\n", expectedSeq-1)
		if m.Content != expectedContent {
			t.Errorf("Message %d: expected content %q, got %q", i, expectedContent, m.Content)
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
	}, nil)

	// User 1 should receive it because EnsureDMsFor added them to the chat
	dmMsg := expectMessages(t, ch1, dmID)
	if len(dmMsg.Messages) == 0 || dmMsg.Messages[0].Content != "<p>hello from new user</p>\n" {
		t.Errorf("Expected 'hello from new user', got %+v", dmMsg.Messages)
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
	}, nil)

	msgLeave := expectMessages(t, ch1b, "townhall")
	if len(msgLeave.Messages) == 0 {
		t.Error("Expected messages on ch1b, got none")
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
	}, nil)

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
	var townhallChat models.Chat
	var found bool
	for _, c := range chats {
		if c.ID == "townhall" {
			townhallChat = c
			found = true
			break
		}
	}
	if !found {
		t.Fatal("townhall chat not found in GetChats")
	}
	if townhallChat.LastSeenSeq != 5 {
		t.Errorf("expected LastSeenSeq 5, got %d", townhallChat.LastSeenSeq)
	}

	// Verify default to LastSeq when no last seen exists (e.g. for User 2)
	chats2 := h.GetChats("u2")
	var townhallChat2 models.Chat
	var found2 bool
	for _, c := range chats2 {
		if c.ID == "townhall" {
			townhallChat2 = c
			found2 = true
			break
		}
	}
	if !found2 {
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
	}, nil)

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
	}, nil)

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

func TestHub_MultipleConnections_ReadReceipt(t *testing.T) {
	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	provider := &MockUserProvider{
		users: []models.User{user1},
	}
	store := NewMockStorage()

	// Setup townhall chat in store with LastSeq 10
	_ = store.UpsertChat(models.Chat{
		ID:      "townhall",
		Name:    "Town Hall",
		LastSeq: 10,
	})

	h := NewHub(context.Background(), provider, store, &MockPushService{})

	// Two connections for the same user
	ch1 := h.Join(user1.ID)
	ch2 := h.Join(user1.ID)

	// User 1 has read up to seq 10 in townhall
	h.Dispatch(user1.ID, models.ClientMessage{
		Type:   models.ClientMessageTypeRead,
		ChatID: "townhall",
		Seq:    10,
	}, ch1)

	// Connection ch2 (another device of the same user) should receive the read receipt
	var readMsgReceived bool
	timeout := time.After(testTimeout)
Loop2:
	for {
		select {
		case msg := <-ch2:
			if msg.Type == models.ServerMessageTypeRead {
				if msg.ChatID != "townhall" || msg.Seq != 10 {
					t.Errorf("expected ServerMessageTypeRead with townhall and seq 10, got ChatID: %s, Seq: %d", msg.ChatID, msg.Seq)
				}
				readMsgReceived = true
				break Loop2
			}
		case <-timeout:
			break Loop2
		}
	}
	if !readMsgReceived {
		t.Fatalf("expected to receive ServerMessageTypeRead on ch2, but did not")
	}

	// Connection ch1 (the sender of the read receipt) should NOT receive the read receipt broadcast
	timeout = time.After(10 * time.Millisecond)
Loop1:
	for {
		select {
		case msg := <-ch1:
			if msg.Type == models.ServerMessageTypeRead {
				t.Errorf("sender connection ch1 should not receive its own read receipt broadcast")
			}
		case <-timeout:
			break Loop1
		}
	}
}

