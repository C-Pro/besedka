package backup

import (
	"bytes"
	"fmt"
	"io"
)

// Backup artifacts are self-describing so they can be decrypted at recovery
// time — before the database (which normally holds the encryption salt) exists.
// Backups are always encrypted, so the salt is always present.
//
// Layout:
//
//	magic   [4]byte = "BSKB"
//	version 1 byte  = 1
//	saltLen 1 byte  = length of salt
//	salt    saltLen bytes
//	payload remaining bytes (encrypted bbolt snapshot)
var magic = [4]byte{'B', 'S', 'K', 'B'}

const headerVersion = 1

type header struct {
	salt []byte
}

// writeHeader writes the artifact header followed by the payload to w.
func writeHeader(w io.Writer, h header, payload []byte) error {
	if len(h.salt) > 255 {
		return fmt.Errorf("backup: salt too long: %d", len(h.salt))
	}
	var buf bytes.Buffer
	buf.Write(magic[:])
	buf.WriteByte(headerVersion)
	buf.WriteByte(byte(len(h.salt)))
	buf.Write(h.salt)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readHeader parses the header from data and returns it along with the payload
// (the remaining bytes).
func readHeader(data []byte) (header, []byte, error) {
	const fixed = 4 + 1 + 1
	if len(data) < fixed {
		return header{}, nil, fmt.Errorf("backup: artifact too short")
	}
	if !bytes.Equal(data[:4], magic[:]) {
		return header{}, nil, fmt.Errorf("backup: bad magic")
	}
	if data[4] != headerVersion {
		return header{}, nil, fmt.Errorf("backup: unsupported version %d", data[4])
	}
	saltLen := int(data[5])
	if len(data) < fixed+saltLen {
		return header{}, nil, fmt.Errorf("backup: truncated salt")
	}
	return header{salt: data[fixed : fixed+saltLen]}, data[fixed+saltLen:], nil
}
