package ws

import (
	"besedka/internal/models"
	"context"
	"errors"
	"testing"
	"time"
)

type mockWS struct {
	readCh      chan models.ClientMessage
	writeCh     chan any
	closeCh     chan struct{}
	closed      bool
	errToReturn error
}

func newMockWS() *mockWS {
	return &mockWS{
		readCh:  make(chan models.ClientMessage, 10),
		writeCh: make(chan any, 10),
		closeCh: make(chan struct{}),
	}
}

func (m *mockWS) Close() error {
	if m.closed {
		return nil
	}
	m.closed = true
	close(m.closeCh)
	return nil
}

func (m *mockWS) WriteJSON(v any) error {
	if m.errToReturn != nil {
		return m.errToReturn
	}
	m.writeCh <- v
	return nil
}

func (m *mockWS) ReadJSON(v any) error {
	if m.errToReturn != nil {
		return m.errToReturn
	}
	select {
	case msg, ok := <-m.readCh:
		if !ok {
			return errors.New("closed")
		}
		// Copy to v (assuming v is *models.ClientMessage)
		if ptr, ok := v.(*models.ClientMessage); ok {
			*ptr = msg
		}
		return nil
	case <-m.closeCh:
		return errors.New("connection closed")
	}
}

type mockHub struct {
	joinCh     chan string
	leaveCh    chan string
	dispatchCh chan models.ClientMessage
	// per user channel
	userChans map[string]chan models.ServerMessage
}

func newMockHub() *mockHub {
	return &mockHub{
		joinCh:     make(chan string, 10),
		leaveCh:    make(chan string, 10),
		dispatchCh: make(chan models.ClientMessage, 10),
		userChans:  make(map[string]chan models.ServerMessage),
	}
}

func (m *mockHub) Join(userID string) chan models.ServerMessage {
	m.joinCh <- userID
	ch := make(chan models.ServerMessage, 10)
	m.userChans[userID] = ch
	return ch
}

func (m *mockHub) Leave(userID string) {
	m.leaveCh <- userID
	if ch, ok := m.userChans[userID]; ok {
		close(ch)
		delete(m.userChans, userID)
	}
}

func (m *mockHub) Dispatch(userID string, msg models.ClientMessage) {
	m.dispatchCh <- msg
}

func TestConnection_Lifecycle(t *testing.T) {
	hub := newMockHub()
	ws := newMockWS()
	userID := "user1"

	conn := NewConnection(hub, ws, userID)
	if conn == nil {
		t.Fatal("NewConnection returned nil")
	}

	// Verify Join was called
	select {
	case id := <-hub.joinCh:
		if id != userID {
			t.Errorf("Expected Join with %s, got %s", userID, id)
		}
	default:
		t.Error("Join not called on NewConnection")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Handle in goroutine
	done := make(chan error)
	go func() {
		done <- conn.Handle(ctx)
	}()

	// 1. Send message from Client -> Hub
	clientMsg := models.ClientMessage{
		Type:    models.ClientMessageTypeSend,
		ChatID:  "chat1",
		Content: "hello",
	}
	ws.readCh <- clientMsg

	select {
	case received := <-hub.dispatchCh:
		if received.Content != clientMsg.Content {
			t.Errorf("Hub received wrong content: %v", received)
		}
	case <-time.After(1 * time.Second):
		t.Error("Hub did not receive dispatched message")
	}

	// 2. Send message from Server -> Client
	serverMsg := models.ServerMessage{
		Type:   models.ServerMessageTypeMessages,
		ChatID: "chat1",
		Messages: []models.Message{
			{Content: "hi back"},
		},
	}
	hub.userChans[userID] <- serverMsg

	select {
	case received := <-ws.writeCh:
		sMsg, ok := received.(models.ServerMessage)
		if !ok {
			t.Fatalf("WS received wrong type: %T", received)
		}
		if len(sMsg.Messages) == 0 || sMsg.Messages[0].Content != "hi back" {
			t.Errorf("WS received wrong content: %v", sMsg)
		}
	case <-time.After(1 * time.Second):
		t.Error("WS did not receive server message")
	}

	// 3. Stop
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Handle returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("Handle did not return after cancel")
	}

	// Verify Leave called
	select {
	case id := <-hub.leaveCh:
		if id != userID {
			t.Errorf("Expected Leave with %s, got %s", userID, id)
		}
	default:
		t.Error("Leave not called")
	}

	// Verify WS Close called
	if !ws.closed {
		t.Error("WS Close not called")
	}
}

func TestConnection_WSError(t *testing.T) {
	hub := newMockHub()
	ws := newMockWS()
	userID := "user2"

	conn := NewConnection(hub, ws, userID)

	// Simulate ReadJSON error immediatelly
	ws.errToReturn = errors.New("read error")

	done := make(chan error)
	go func() {
		done <- conn.Handle(context.Background())
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Expected error from Handle, got nil")
		}
	case <-time.After(1 * time.Second):
		t.Error("Handle did not return on error")
	}

	if !ws.closed {
		t.Error("WS Close not called")
	}
}
