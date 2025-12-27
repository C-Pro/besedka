package ws

import (
	"besedka/internal/models"
	"testing"
	"time"
)

func TestHub_Lifecycle(t *testing.T) {
	h := NewHub()

	user1 := models.User{ID: "u1", DisplayName: "User 1"}
	user2 := models.User{ID: "u2", DisplayName: "User 2"}

	// 1. Add Users
	h.AddUser(user1)
	h.AddUser(user2)

	// Verify chats created
	// Townhall
	if _, ok := h.chats["townhall"]; !ok {
		t.Error("Townhall not created")
	}
	// DM
	dmID := getDMID(user1.ID, user2.ID)
	if _, ok := h.chats[dmID]; !ok {
		t.Errorf("DM %s not created", dmID)
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
