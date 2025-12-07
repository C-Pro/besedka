package chat

import (
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	c := New(Config{MaxRecords: 10})
	if c == nil {
		t.Fatal("New returned nil")
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
	c.RecordCallback = func(id string, r ChatRecord) {}

	for i := 0; i < 5; i++ {
		c.AddRecord(ChatRecord{UserID: "user", Content: fmt.Sprintf("msg %d", i)})
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
	c.RecordCallback = func(id string, r ChatRecord) {}

	// Add 3 records (full)
	for i := 0; i < 3; i++ {
		c.AddRecord(ChatRecord{UserID: "user", Content: fmt.Sprintf("msg %d", i)})
	}

	// Add 1 more (wrap)
	c.AddRecord(ChatRecord{UserID: "user", Content: "msg 3"})

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
	c := New(Config{MaxRecords: 10})

	// Setup members
	c.Join("online_user")
	c.Members["offline_user"] = false // Manually set offline user

	received := make(map[string]ChatRecord)
	c.RecordCallback = func(receiverID string, r ChatRecord) {
		received[receiverID] = r
	}

	msg := ChatRecord{UserID: "sender", Content: "hello"}
	c.AddRecord(msg)

	// Check online user received it
	if rec, ok := received["online_user"]; !ok {
		t.Error("online_user did not receive message")
	} else if rec.Content != "hello" {
		t.Errorf("online_user received wrong content: %s", rec.Content)
	}

	// Check offline user did not receive it
	if _, ok := received["offline_user"]; ok {
		t.Error("offline_user received message but shouldn't have")
	}
}
