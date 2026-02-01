package storage

import (
	"encoding"
	"encoding/binary"

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
	return []byte(t.UserID)
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

type DBChat struct {
	ID      string `msgpack:"id"`
	Name    string `msgpack:"name"`
	LastSeq int    `msgpack:"lastSeq"`
	IsDM    bool   `msgpack:"isDm"`
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
