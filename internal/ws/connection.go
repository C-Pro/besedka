package ws

import (
	"besedka/internal/content"
	"besedka/internal/models"
	"context"
	"errors"
	"sync"
	"time"
)

const (
	pingInterval = 30 * time.Second
	readDeadline = 60 * time.Second
)

type wsConnection interface {
	Close() error
	WriteJSON(v any) error
	ReadJSON(v any) error
	SetReadDeadline(t time.Time) error
}

type messageHub interface {
	Join(userID string) chan models.ServerMessage
	Leave(userID string)
	Dispatch(userID string, msg models.ClientMessage)
}

type Connection struct {
	ws         wsConnection
	hub        messageHub
	userID     string
	fromClient chan models.ClientMessage
	fromServer chan models.ServerMessage
	errorCh    chan error
}

func NewConnection(
	hub messageHub,
	ws wsConnection,
	userID string,
) *Connection {
	return &Connection{
		ws:         ws,
		hub:        hub,
		userID:     userID,
		fromClient: make(chan models.ClientMessage),
		fromServer: hub.Join(userID),
		errorCh:    make(chan error, 2),
	}
}

func (c *Connection) Handle(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		close(c.fromClient)
		close(c.errorCh)
		c.hub.Leave(c.userID)
	}()

	var wg sync.WaitGroup
	wg.Go(func() {
		c.errorCh <- c.pumpMessages(ctx)
		cancel()
	})

	wg.Go(func() {
		c.errorCh <- c.mainLoop(ctx)
		cancel()
	})

	var err error
	select {
	case err = <-c.errorCh:
	case <-ctx.Done():
	}
	_ = c.ws.Close()
	wg.Wait()

	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func (c *Connection) pumpMessages(ctx context.Context) error {
	if err := c.ws.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
		return err
	}
	for {
		var msg models.ClientMessage
		if err := c.ws.ReadJSON(&msg); err != nil {
			return err
		}
		if err := c.ws.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
			return err
		}
		select {
		case c.fromClient <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *Connection) mainLoop(ctx context.Context) error {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg := <-c.fromClient:
			if err := c.processClientMessage(msg); err != nil {
				return err
			}
		case msg := <-c.fromServer:
			if err := c.ws.WriteJSON(msg); err != nil {
				return err
			}
		case <-ticker.C:
			if err := c.ws.WriteJSON(models.ServerMessage{Type: models.ServerMessageTypePing}); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}

}

func (c *Connection) processClientMessage(msg models.ClientMessage) error {
	switch msg.Type {
	case models.ClientMessageTypeJoin, models.ClientMessageTypeSend, models.ClientMessageTypeFetch:
		msg.Content = content.Sanitize(msg.Content)
		c.hub.Dispatch(c.userID, msg)
	}

	return nil
}
