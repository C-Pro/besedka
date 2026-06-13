package storage

import (
	"encoding"
	"encoding/binary"

	"besedka/internal/models"

	"github.com/vmihailenco/msgpack/v5"
)

type Storeable interface {
	Key() []byte
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

type DBToken struct {
	UserID string `msgpack:"userId"`
	Token  string `msgpack:"token"`
}

func (t *DBToken) Key() []byte {
	return []byte(t.Token)
}

func (t *DBToken) MarshalBinary() (data []byte, err error) {
	type alias DBToken
	return msgpack.Marshal((*alias)(t))
}

func (t *DBToken) UnmarshalBinary(data []byte) error {
	type alias DBToken
	return msgpack.Unmarshal(data, (*alias)(t))
}

type DBUser struct {
	ID           string `msgpack:"id"`
	UserName     string `msgpack:"userName"`
	DisplayName  string `msgpack:"displayName"`
	AvatarURL    string `msgpack:"avatarUrl"`
	LastSeen     int64  `msgpack:"lastSeen"`
	PasswordHash string `msgpack:"passwordHash"`
	TOTPSecret   string `msgpack:"totpSecret"`
	LastTOTP     int    `msgpack:"lastTOTP"`
	Status       string `msgpack:"status"`
}

func (u *DBUser) Key() []byte {
	return []byte(u.ID)
}

func (u *DBUser) MarshalBinary() (data []byte, err error) {
	type alias DBUser
	return msgpack.Marshal((*alias)(u))
}

func (u *DBUser) UnmarshalBinary(data []byte) error {
	type alias DBUser
	return msgpack.Unmarshal(data, (*alias)(u))
}

type DBNotificationSettings struct {
	SoundAllMessages     bool `msgpack:"soundAllMessages"`
	SoundDirectMessages  bool `msgpack:"soundDirectMessages"`
	SoundMentions        bool `msgpack:"soundMentions"`
	SuppressWhenChatOpen bool `msgpack:"suppressWhenChatOpen"`
}

type DBUserSettings struct {
	UserID        string                 `msgpack:"userId"`
	Notifications DBNotificationSettings `msgpack:"notifications"`
}

func (s *DBUserSettings) Key() []byte {
	return []byte(s.UserID)
}

func (s *DBUserSettings) MarshalBinary() (data []byte, err error) {
	type alias DBUserSettings
	return msgpack.Marshal((*alias)(s))
}

func (s *DBUserSettings) UnmarshalBinary(data []byte) error {
	type alias DBUserSettings
	return msgpack.Unmarshal(data, (*alias)(s))
}

func userSettingsToDB(userID string, s models.UserSettings) *DBUserSettings {
	return &DBUserSettings{
		UserID: userID,
		Notifications: DBNotificationSettings{
			SoundAllMessages:     s.Notifications.SoundAllMessages,
			SoundDirectMessages:  s.Notifications.SoundDirectMessages,
			SoundMentions:        s.Notifications.SoundMentions,
			SuppressWhenChatOpen: s.Notifications.SuppressWhenChatOpen,
		},
	}
}

func (s *DBUserSettings) toModel() models.UserSettings {
	return models.UserSettings{
		Notifications: models.NotificationSettings{
			SoundAllMessages:     s.Notifications.SoundAllMessages,
			SoundDirectMessages:  s.Notifications.SoundDirectMessages,
			SoundMentions:        s.Notifications.SoundMentions,
			SuppressWhenChatOpen: s.Notifications.SuppressWhenChatOpen,
		},
	}
}

type DBChat struct {
	ID        string `msgpack:"id"`
	Name      string `msgpack:"name"`
	AvatarURL string `msgpack:"avatarUrl"`
	LastSeq   int    `msgpack:"lastSeq"`
	IsDM      bool   `msgpack:"isDm"`
}

func (c *DBChat) Key() []byte {
	return []byte(c.ID)
}

func (c *DBChat) MarshalBinary() (data []byte, err error) {
	type alias DBChat
	return msgpack.Marshal((*alias)(c))
}

func (c *DBChat) UnmarshalBinary(data []byte) error {
	type alias DBChat
	return msgpack.Unmarshal(data, (*alias)(c))
}

type DBMessage struct {
	Seq         int64          `msgpack:"seq"`
	Timestamp   int64          `msgpack:"timestamp"`
	ChatID      string         `msgpack:"chatId"`
	UserID      string         `msgpack:"userId"`
	Content     string         `msgpack:"content"`
	Attachments []DBAttachment `msgpack:"attachments"`
}

type DBAttachment struct {
	Type     string `msgpack:"type"`
	Name     string `msgpack:"name"`
	MimeType string `msgpack:"mimeType"`
	FileID   string `msgpack:"fileId"`
}

func (m *DBMessage) Key() []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(m.Seq))
	return key
}

func (m *DBMessage) MarshalBinary() (data []byte, err error) {
	type alias DBMessage
	return msgpack.Marshal((*alias)(m))
}

func (m *DBMessage) UnmarshalBinary(data []byte) error {
	type alias DBMessage
	return msgpack.Unmarshal(data, (*alias)(m))
}

type DBVAPIDKeys struct {
	PrivateKey string `msgpack:"privateKey"`
	PublicKey  string `msgpack:"publicKey"`
}

func (k *DBVAPIDKeys) Key() []byte {
	return []byte("vapid_keys")
}

func (k *DBVAPIDKeys) MarshalBinary() (data []byte, err error) {
	type alias DBVAPIDKeys
	return msgpack.Marshal((*alias)(k))
}

func (k *DBVAPIDKeys) UnmarshalBinary(data []byte) error {
	type alias DBVAPIDKeys
	return msgpack.Unmarshal(data, (*alias)(k))
}

type DBPushSubscription struct {
	UserID   string `msgpack:"userId"`
	Endpoint string `msgpack:"endpoint"`
	Data     []byte `msgpack:"data"`
}

func (s *DBPushSubscription) Key() []byte {
	return []byte(s.Endpoint)
}

func (s *DBPushSubscription) MarshalBinary() (data []byte, err error) {
	type alias DBPushSubscription
	return msgpack.Marshal((*alias)(s))
}

func (s *DBPushSubscription) UnmarshalBinary(data []byte) error {
	type alias DBPushSubscription
	return msgpack.Unmarshal(data, (*alias)(s))
}

type DBLastSeen struct {
	UserID string `msgpack:"userId"`
	ChatID string `msgpack:"chatId"`
	Seq    int64  `msgpack:"seq"`
}

func (l *DBLastSeen) Key() []byte {
	return []byte(l.UserID + ":" + l.ChatID)
}

func (l *DBLastSeen) MarshalBinary() (data []byte, err error) {
	type alias DBLastSeen
	return msgpack.Marshal((*alias)(l))
}

func (l *DBLastSeen) UnmarshalBinary(data []byte) error {
	type alias DBLastSeen
	return msgpack.Unmarshal(data, (*alias)(l))
}

type DBPasskeyCredential struct {
	ID              []byte   `msgpack:"id"`
	UserID          string   `msgpack:"userId"`
	PublicKey       []byte   `msgpack:"publicKey"`
	AttestationType string   `msgpack:"attestationType"`
	AAGUID          []byte   `msgpack:"aaguid"`
	SignCount       uint32   `msgpack:"signCount"`
	Name            string   `msgpack:"name"`
	CreatedAt       int64    `msgpack:"createdAt"`
	Transport       []string `msgpack:"transport"`
	BackupEligible  bool     `msgpack:"backupEligible"`
	BackupState     bool     `msgpack:"backupState"`
}

func (c *DBPasskeyCredential) Key() []byte {
	return c.ID
}

func (c *DBPasskeyCredential) MarshalBinary() (data []byte, err error) {
	type alias DBPasskeyCredential
	return msgpack.Marshal((*alias)(c))
}

func (c *DBPasskeyCredential) UnmarshalBinary(data []byte) error {
	type alias DBPasskeyCredential
	return msgpack.Unmarshal(data, (*alias)(c))
}
