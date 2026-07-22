package backup

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Backup artifacts are self-describing so they can be decrypted at recovery
// time — before the database (which normally holds the encryption salt) exists.
// Backups are always encrypted, so the salt is always present.
//
// Version 1 (full snapshots — kept byte-identical so binaries that predate
// incremental backups can still restore the newest full):
//
//	magic   [4]byte = "BSKB"
//	version 1 byte  = 1
//	saltLen 1 byte  = length of salt
//	salt    saltLen bytes
//	payload remaining bytes (encrypted bbolt snapshot)
//
// Version 2 (incremental snapshots) additionally records the artifact kind and
// the object key of the parent artifact, so recovery can verify the chain is
// unbroken before applying it:
//
//	magic     [4]byte = "BSKB"
//	version   1 byte  = 2
//	kind      1 byte  (0 = full, 1 = incremental)
//	saltLen   1 byte
//	salt      saltLen bytes
//	parentLen 2 bytes big-endian
//	parent    parentLen bytes (object key of the parent artifact)
//	payload   remaining bytes (encrypted incremental entry stream)
var magic = [4]byte{'B', 'S', 'K', 'B'}

const (
	headerVersion1 = 1
	headerVersion2 = 2
)

const (
	kindFull        byte = 0
	kindIncremental byte = 1
)

type header struct {
	version byte
	kind    byte
	salt    []byte
	parent  string
}

// writeHeader writes the artifact header followed by the payload to w. A zero
// h.version writes version 1 (a full snapshot).
func writeHeader(w io.Writer, h header, payload []byte) error {
	if len(h.salt) > 255 {
		return fmt.Errorf("backup: salt too long: %d", len(h.salt))
	}
	var buf bytes.Buffer
	buf.Write(magic[:])
	switch h.version {
	case 0, headerVersion1:
		buf.WriteByte(headerVersion1)
	case headerVersion2:
		if len(h.parent) > 0xFFFF {
			return fmt.Errorf("backup: parent key too long: %d", len(h.parent))
		}
		buf.WriteByte(headerVersion2)
		buf.WriteByte(h.kind)
	default:
		return fmt.Errorf("backup: unsupported header version %d", h.version)
	}
	buf.WriteByte(byte(len(h.salt)))
	buf.Write(h.salt)
	if h.version == headerVersion2 {
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(h.parent)))
		buf.Write(l[:])
		buf.WriteString(h.parent)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readHeader parses the header from data and returns it along with the payload
// (the remaining bytes). Version 1 artifacts are reported as kind full with no
// parent.
func readHeader(data []byte) (header, []byte, error) {
	if len(data) < 5 {
		return header{}, nil, fmt.Errorf("backup: artifact too short")
	}
	if !bytes.Equal(data[:4], magic[:]) {
		return header{}, nil, fmt.Errorf("backup: bad magic")
	}
	switch data[4] {
	case headerVersion1:
		const fixed = 4 + 1 + 1
		if len(data) < fixed {
			return header{}, nil, fmt.Errorf("backup: artifact too short")
		}
		saltLen := int(data[5])
		if len(data) < fixed+saltLen {
			return header{}, nil, fmt.Errorf("backup: truncated salt")
		}
		h := header{version: headerVersion1, kind: kindFull, salt: data[fixed : fixed+saltLen]}
		return h, data[fixed+saltLen:], nil
	case headerVersion2:
		const fixed = 4 + 1 + 1 + 1
		if len(data) < fixed {
			return header{}, nil, fmt.Errorf("backup: artifact too short")
		}
		kind := data[5]
		if kind != kindFull && kind != kindIncremental {
			return header{}, nil, fmt.Errorf("backup: unknown artifact kind %d", kind)
		}
		saltLen := int(data[6])
		if len(data) < fixed+saltLen+2 {
			return header{}, nil, fmt.Errorf("backup: truncated salt")
		}
		salt := data[fixed : fixed+saltLen]
		parentLen := int(binary.BigEndian.Uint16(data[fixed+saltLen : fixed+saltLen+2]))
		rest := data[fixed+saltLen+2:]
		if len(rest) < parentLen {
			return header{}, nil, fmt.Errorf("backup: truncated parent key")
		}
		h := header{version: headerVersion2, kind: kind, salt: salt, parent: string(rest[:parentLen])}
		return h, rest[parentLen:], nil
	default:
		return header{}, nil, fmt.Errorf("backup: unsupported version %d", data[4])
	}
}
