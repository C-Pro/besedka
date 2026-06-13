package models

import "errors"

var (
	ErrNotFound = errors.New("not found")
)

// APIResponse represents a standard API response.
type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// ResetPasswordResponse represents a response for password reset operations.
type ResetPasswordResponse struct {
	APIResponse
	SetupLink string `json:"setupLink"`
}

// UploadImageResponse represents a response for an image upload operation.
type UploadImageResponse struct {
	ID string `json:"id"`
}

// UploadFileResponse represents a response for a generic file upload operation.
type UploadFileResponse struct {
	ID string `json:"id"`
}

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

// NotificationSettings controls when an incoming-message notification sound is played.
type NotificationSettings struct {
	SoundAllMessages     bool `json:"soundAllMessages"`
	SoundDirectMessages  bool `json:"soundDirectMessages"`
	SoundMentions        bool `json:"soundMentions"`
	SuppressWhenChatOpen bool `json:"suppressWhenChatOpen"`
}

// UserSettings holds a user's persisted preferences. It is the single
// extensibility point for per-user settings; add sibling sections here as
// new preference groups are introduced.
type UserSettings struct {
	Notifications NotificationSettings `json:"notifications"`
}

// DefaultUserSettings returns the settings applied to a user who has never
// saved any preferences. Direct-message and mention sounds are on by default,
// "all messages" is off, and sounds are muted for the chat already on screen.
func DefaultUserSettings() UserSettings {
	return UserSettings{
		Notifications: NotificationSettings{
			SoundAllMessages:     false,
			SoundDirectMessages:  true,
			SoundMentions:        true,
			SuppressWhenChatOpen: true,
		},
	}
}

// Chat represents a chat conversation.
type Chat struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	LastSeq     int    `json:"lastSeq"` // Last message sequence number (used to backfill messages and show unread count)
	IsDM        bool   `json:"isDm"`
	Online      bool   `json:"online,omitempty"` // Optional, for DMs
	LastSeenSeq int64  `json:"lastSeenSeq"`      // Persistent last seen sequence number
}

// LastSeenEntry represents a persisted last seen sequence number.
type LastSeenEntry struct {
	UserID string `json:"userId"`
	ChatID string `json:"chatId"`
	Seq    int64  `json:"seq"`
}

// Message represents a chat message.
type Message struct {
	Seq         int64        `json:"seq"`
	Timestamp   int64        `json:"timestamp"` // Unix timestamp (seconds)
	ChatID      string       `json:"chatId"`
	UserID      string       `json:"userId"`
	Content     string       `json:"content"`
	RawContent  string       `json:"rawContent,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Location represents geographic coordinates.
type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// UserLocation represents a user's location.
type UserLocation struct {
	UserID   string   `json:"userId"`
	Location Location `json:"location"`
}

// ClientMessage represents a message sent from the client to the server.
type ClientMessage struct {
	Type        ClientMessageType `json:"type"`
	ChatID      string            `json:"chatId,omitempty"`
	Content     string            `json:"content,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	FromSeq     int64             `json:"fromSeq,omitempty"`
	ToSeq       int64             `json:"toSeq,omitempty"`
	Location    *Location         `json:"location,omitempty"`
	Seq         int64             `json:"seq,omitempty"` // Sequence number for read receipts
}

// ServerMessage represents a message to the client.
type ServerMessage struct {
	Type          ServerMessageType `json:"type"`
	UserID        string            `json:"userId,omitempty"`
	Online        bool              `json:"online,omitempty"`
	ChatID        string            `json:"chatId,omitempty"`
	Messages      []Message         `json:"messages,omitempty"`
	User          User              `json:"user,omitempty"`
	Chat          Chat              `json:"chat,omitempty"`
	UserLocations []UserLocation    `json:"userLocations,omitempty"`
	Seq           int64             `json:"seq,omitempty"`
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
	ClientMessageTypeJoin     ClientMessageType = "join"
	ClientMessageTypeLeave    ClientMessageType = "leave"
	ClientMessageTypeSend     ClientMessageType = "send"
	ClientMessageTypeFetch    ClientMessageType = "fetch"
	ClientMessageTypePong     ClientMessageType = "pong"
	ClientMessageTypeLocation ClientMessageType = "location"
	ClientMessageTypeRead     ClientMessageType = "read"
)

type ServerMessageType string

const (
	ServerMessageTypeOnline   ServerMessageType = "online"
	ServerMessageTypeOffline  ServerMessageType = "offline"
	ServerMessageTypeMessages ServerMessageType = "messages"
	// Sent when a new user is created
	ServerMessageTypeNew ServerMessageType = "new"
	// Sent when user is deleted
	ServerMessageTypeDeleted  ServerMessageType = "deleted"
	ServerMessageTypePing     ServerMessageType = "ping"
	ServerMessageTypeLocation ServerMessageType = "location"
	ServerMessageTypeRead     ServerMessageType = "read"
)
