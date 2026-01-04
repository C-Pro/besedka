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

type DBUser struct {
	ID                  string `msgpack:"id"`
	UserName            string `msgpack:"userName"`
	DisplayName         string `msgpack:"displayName"`
	AvatarURL           string `msgpack:"avatarUrl"`
	LastSeen            int64  `msgpack:"lastSeen"`
	PasswordHash        string `msgpack:"passwordHash"`
	TOTPSecret          string `msgpack:"totpSecret"`
	LastTOTP            int    `msgpack:"lastTOTP"`
}

func (u *DBUser) Key() []byte {
	return []byte(u.ID)
}

func (u *DBUser) MarshalBinary() (data []byte, err error) {
	type Alias DBUser
	return msgpack.Marshal((*Alias)(u))
}

func (u *DBUser) UnmarshalBinary(data []byte) error {
	type Alias DBUser
	return msgpack.Unmarshal(data, (*Alias)(u))
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
	type Alias DBChat
	return msgpack.Marshal((*Alias)(c))
}

func (c *DBChat) UnmarshalBinary(data []byte) error {
	type Alias DBChat
	return msgpack.Unmarshal(data, (*Alias)(c))
}

type DBMessage struct {
	Seq       int64  `msgpack:"seq"`
	Timestamp int64  `msgpack:"timestamp"`
	ChatID    string `msgpack:"chatId"`
	UserID    string `msgpack:"userId"`
	Content   string `msgpack:"content"`
}

func (m *DBMessage) Key() []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(m.Seq))
	return key
}

func (m *DBMessage) MarshalBinary() (data []byte, err error) {
	type Alias DBMessage
	return msgpack.Marshal((*Alias)(m))
}

func (m *DBMessage) UnmarshalBinary(data []byte) error {
	type Alias DBMessage
	return msgpack.Unmarshal(data, (*Alias)(m))
}
