package models

// User represents a user in the system.
type User struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	AvatarURL   string   `json:"avatarUrl"`
	Presence    Presence `json:"presence"`
}

// Presence represents the online status of a user.
type Presence struct {
	Online   bool   `json:"online"`
	LastSeen string `json:"lastSeen"` // Unix timestamp as string
}

// Chat represents a chat conversation.
type Chat struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UnreadCount int    `json:"unreadCount"`
	IsDM        bool   `json:"isDm"`
	Online      bool   `json:"online,omitempty"` // Optional, for DMs
}

// Message represents a chat message.
type Message struct {
	Timestamp string `json:"timestamp"` // Unix timestamp as string
	UserID    string `json:"userId"`
	Content   string `json:"content"`
}

// ClientMessage represents a message sent from the client to the server.
type ClientMessage struct {
	Type    ClientMessageType `json:"type"`
	ChatID  string            `json:"chatId"`
	Content string            `json:"content"`
}

// ServerMessage represents a message to the client.
type ServerMessage struct {
	Type     ServerMessageType `json:"type"`
	UserID   string            `json:"userId,omitempty"`
	Online   bool              `json:"online,omitempty"`
	ChatID   string            `json:"chatId,omitempty"`
	Messages []Message         `json:"messages,omitempty"`
}

type ClientMessageType string

const (
	ClientMessageTypeJoin  ClientMessageType = "join"
	ClientMessageTypeLeave ClientMessageType = "leave"
	ClientMessageTypeSend  ClientMessageType = "send"
)

type ServerMessageType string

const (
	ServerMessageTypeJoin     ServerMessageType = "join"
	ServerMessageTypeLeave    ServerMessageType = "leave"
	ServerMessageTypeSend     ServerMessageType = "send"
	ServerMessageTypeOnline   ServerMessageType = "online"
	ServerMessageTypeOffline  ServerMessageType = "offline"
	ServerMessageTypeMessages ServerMessageType = "messages"
)
