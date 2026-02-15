package models

import "errors"

var (
	ErrNotFound = errors.New("not found")
)

type UserStatus string

const (
	UserStatusCreated UserStatus = "created"
	UserStatusActive  UserStatus = "active"
	UserStatusDeleted UserStatus = "deleted"
)

// User represents a user in the system.
type User struct {
	ID          string     `json:"id"`
	UserName    string     `json:"userName"`
	DisplayName string     `json:"displayName"`
	AvatarURL   string     `json:"avatarUrl"`
	Presence    Presence   `json:"presence"`
	Status      UserStatus `json:"status"`
}

// Presence represents the online status of a user.
type Presence struct {
	Online   bool  `json:"online"`
	LastSeen int64 `json:"lastSeen"` // Unix timestamp (seconds)
}

// Chat represents a chat conversation.
type Chat struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	LastSeq int    `json:"lastSeq"` // Last message sequence number (used to backfill messages and show unread count)
	IsDM    bool   `json:"isDm"`
	Online  bool   `json:"online,omitempty"` // Optional, for DMs
}

// Message represents a chat message.
type Message struct {
	Seq         int64        `json:"seq"`
	Timestamp   int64        `json:"timestamp"` // Unix timestamp (seconds)
	ChatID      string       `json:"chatId"`
	UserID      string       `json:"userId"`
	Content     string       `json:"content"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// ClientMessage represents a message sent from the client to the server.
type ClientMessage struct {
	Type        ClientMessageType `json:"type"`
	ChatID      string            `json:"chatId"`
	Content     string            `json:"content"`
	Attachments []Attachment      `json:"attachments,omitempty"`
}

// ServerMessage represents a message to the client.
type ServerMessage struct {
	Type     ServerMessageType `json:"type"`
	UserID   string            `json:"userId,omitempty"`
	Online   bool              `json:"online,omitempty"`
	ChatID   string            `json:"chatId,omitempty"`
	Messages []Message         `json:"messages,omitempty"`
}

type AttachmentType string

const (
	AttachmentTypeImage AttachmentType = "image"
	AttachmentTypeFile  AttachmentType = "file"
)

type Attachment struct {
	Type     AttachmentType `json:"type"`
	Name     string         `json:"name"`
	MimeType string         `json:"mimeType"`
	FileID   string         `json:"fileId"`
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
