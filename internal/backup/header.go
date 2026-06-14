package backup

import (
	"bytes"
	"fmt"
	"io"
)

// Backup artifacts are self-describing so they can be decrypted at recovery
// time — before the database (which normally holds the encryption salt) exists.
//
// Layout:
//
//	magic   [4]byte = "BSKB"
//	version 1 byte  = 1
//	encFlag 1 byte  = 0 (plaintext) | 1 (encrypted)
//	saltLen 1 byte  = length of salt (0 when plaintext)
//	salt    saltLen bytes
//	payload remaining bytes (bbolt snapshot, possibly encrypted)
var magic = [4]byte{'B', 'S', 'K', 'B'}

const headerVersion = 1

type header struct {
	encrypted bool
	salt      []byte
}

// writeHeader writes the artifact header followed by the payload to w.
func writeHeader(w io.Writer, h header, payload []byte) error {
	var buf bytes.Buffer
	buf.Write(magic[:])
	buf.WriteByte(headerVersion)
	if h.encrypted {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	if len(h.salt) > 255 {
		return fmt.Errorf("backup: salt too long: %d", len(h.salt))
	}
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
	const fixed = 4 + 1 + 1 + 1
	if len(data) < fixed {
		return header{}, nil, fmt.Errorf("backup: artifact too short")
	}
	if !bytes.Equal(data[:4], magic[:]) {
		return header{}, nil, fmt.Errorf("backup: bad magic")
	}
	if data[4] != headerVersion {
		return header{}, nil, fmt.Errorf("backup: unsupported version %d", data[4])
	}
	h := header{encrypted: data[5] == 1}
	saltLen := int(data[6])
	if len(data) < fixed+saltLen {
		return header{}, nil, fmt.Errorf("backup: truncated salt")
	}
	h.salt = data[fixed : fixed+saltLen]
	return h, data[fixed+saltLen:], nil
}
