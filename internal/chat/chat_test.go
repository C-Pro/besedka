package chat

import (
	"besedka/internal/models"
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	c := New(Config{MaxRecords: 10})
	if c == nil {
		t.Fatal("New returned nil")
		return
	}
	if c.MaxRecords != 10 {
		t.Errorf("expected MaxRecords 10, got %d", c.MaxRecords)
	}
	// Check initialization of map
	if c.Members == nil {
		t.Error("Members map not initialized")
	}
}

func TestChat_AddRecord_NoWrap(t *testing.T) {
	c := New(Config{MaxRecords: 10})
	c.RecordCallback = func(id string, chatID string, r ChatRecord) {}

	for i := 0; i < 5; i++ {
		if err := c.AddRecord(ChatRecord{UserID: "user", Content: fmt.Sprintf("msg %d", i)}); err != nil {
			t.Errorf("AddRecord failed: %v", err)
		}
	}

	if len(c.Records) != 5 {
		t.Errorf("expected 5 records, got %d", len(c.Records))
	}

	// Test GetLastRecords
	recs, err := c.GetLastRecords(2)
	if err != nil {
		t.Fatalf("GetLastRecords failed: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("expected 2 records, got %d", len(recs))
	}
	if recs[1].Content != "msg 4" {
		t.Errorf("expected last msg 'msg 4', got '%s'", recs[1].Content)
	}
}

func TestChat_AddRecord_Wrap(t *testing.T) {
	c := New(Config{MaxRecords: 3})
	if c.Members == nil {
		c.Members = make(map[string]bool)
	}
	c.RecordCallback = func(id string, chatID string, r ChatRecord) {}

	// Add 3 records (full)
	for i := 0; i < 3; i++ {
		if err := c.AddRecord(ChatRecord{UserID: "user", Content: fmt.Sprintf("msg %d", i)}); err != nil {
			t.Errorf("AddRecord failed: %v", err)
		}
	}

	// Add 1 more (wrap)
	if err := c.AddRecord(ChatRecord{UserID: "user", Content: "msg 3"}); err != nil {
		t.Errorf("AddRecord failed: %v", err)
	}

	// Test GetLastRecords
	recs, err := c.GetLastRecords(3)
	if err != nil {
		t.Fatalf("GetLastRecords failed: %v", err)
	}

	// Expect chronological order: msg 1, msg 2, msg 3
	// msg 0 should be dropped
	expected := []string{"msg 1", "msg 2", "msg 3"}
	for i, exp := range expected {
		if recs[i].Content != exp {
			t.Errorf("index %d: expected '%s', got '%s'", i, exp, recs[i].Content)
		}
	}
}

func TestChat_JoinLeave(t *testing.T) {
	c := New(Config{MaxRecords: 10})
	if c.Members == nil {
		c.Members = make(map[string]bool)
	}

	c.Join("user1")
	if !c.Members["user1"] {
		t.Error("user1 should be online")
	}

	c.Leave("user1")
	if c.Members["user1"] {
		t.Error("user1 should be offline")
	}
}

func TestChat_Callback(t *testing.T) {
	c := New(Config{ID: "chat1", MaxRecords: 10})

	// Setup members
	c.Join("online_user")
	c.Members["offline_user"] = false // Manually set offline user

	received := make(map[string]ChatRecord)
	c.RecordCallback = func(receiverID string, chatID string, r ChatRecord) {
		received[chatID+":"+receiverID] = r
	}

	msg := ChatRecord{UserID: "sender", Content: "hello"}
	if err := c.AddRecord(msg); err != nil {
		t.Errorf("AddRecord failed: %v", err)
	}

	// Check online user received it
	if rec, ok := received["chat1:online_user"]; !ok {
		t.Error("online_user did not receive message")
	} else if rec.Content != "hello" {
		t.Errorf("online_user received wrong content: %s", rec.Content)
	}

	// Check offline user did not receive it
	if _, ok := received["chat1:offline_user"]; ok {
		t.Error("offline_user received message but shouldn't have")
	}
}

// MockStorage for testing
type MockStorage struct {
	messages map[string][]ChatRecord
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		messages: make(map[string][]ChatRecord),
	}
}

func (m *MockStorage) UpsertMessage(msg models.Message) error {
	m.messages[msg.ChatID] = append(m.messages[msg.ChatID], ChatRecord{
		Seq:       Seq(msg.Seq),
		Timestamp: msg.Timestamp,
		UserID:    msg.UserID,
		Content:   msg.Content,
	})
	return nil
}

func (m *MockStorage) ListMessages(chatID string, from, to int64) ([]models.Message, error) {
	var results []models.Message
	if msgs, ok := m.messages[chatID]; ok {
		for _, r := range msgs {
			if int64(r.Seq) >= from && int64(r.Seq) <= to {
				results = append(results, models.Message{
					Seq:       int64(r.Seq),
					Timestamp: r.Timestamp,
					ChatID:    chatID,
					UserID:    r.UserID,
					Content:   r.Content,
				})
			}
		}
	}
	return results, nil
}

func TestChat_Persistence(t *testing.T) {
	store := NewMockStorage()
	c := New(Config{
		ID:         "chat_persist",
		MaxRecords: 5,
		Storage:    store,
	})

	// Add 10 records. MaxRecords is 5.
	// So 5 should be in memory, all 10 in storage.
	for i := 1; i <= 10; i++ {
		if err := c.AddRecord(ChatRecord{
			UserID:  "user",
			Content: fmt.Sprintf("msg %d", i),
		}); err != nil {
			t.Errorf("AddRecord failed: %v", err)
		}
	}

	// Verify Check memory
	if len(c.Records) != 5 {
		t.Errorf("expected 5 records in memory, got %d", len(c.Records))
	}
	// Memory should have msg 6 to 10. Seq 6 to 10.
	// FirstSeq should be 6.
	if c.FirstSeq != 6 {
		t.Errorf("expected FirstSeq 6, got %d", c.FirstSeq)
	}

	// Verify Storage
	if len(store.messages["chat_persist"]) != 10 {
		t.Errorf("expected 10 messages in storage, got %d", len(store.messages["chat_persist"]))
	}

	// Test GetRecords covering storage and memory
	// Ask for 1 to 10.
	// 1-5 from storage.
	// 6-10 from memory.
	recs, err := c.GetRecords(1, 11) // [1, 11) -> 1..10
	if err != nil {
		t.Fatalf("GetRecords failed: %v", err)
	}

	if len(recs) != 10 {
		t.Errorf("expected 10 records, got %d", len(recs))
	}

	for i, r := range recs {
		expectedContent := fmt.Sprintf("msg %d", i+1)
		if r.Content != expectedContent {
			t.Errorf("index %d: expected content '%s', got '%s'", i, expectedContent, r.Content)
		}
		if r.Seq != Seq(i+1) {
			t.Errorf("index %d: expected seq %d, got %d", i, i+1, r.Seq)
		}
	}

	// Test GetRecords pure storage
	recs, err = c.GetRecords(1, 3) // [1, 3) -> 1, 2
	if err != nil {
		t.Fatalf("GetRecords storage only failed: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Content != "msg 1" {
		t.Errorf("expected msg 1, got %s", recs[0].Content)
	}

}
