package audio

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractMetadata_ID3v23(t *testing.T) {
	// Build synthetic ID3v2.3 header and frames
	// ID3 header: "ID3" + version(3,0) + flags(0) + size(synchsafe 7-bit)
	// Frame 1: TIT2 (Title)
	// Frame 2: TPE1 (Artist)

	titleStr := "Test Song Title"
	artistStr := "Test Artist"

	// TIT2 payload: encoding 0 (ISO-8859-1) + string
	tit2Payload := append([]byte{0}, []byte(titleStr)...)
	tit2Frame := make([]byte, 10+len(tit2Payload))
	copy(tit2Frame[:4], "TIT2")
	binary.BigEndian.PutUint32(tit2Frame[4:8], uint32(len(tit2Payload)))
	copy(tit2Frame[10:], tit2Payload)

	// TPE1 payload: encoding 0 + string
	tpe1Payload := append([]byte{0}, []byte(artistStr)...)
	tpe1Frame := make([]byte, 10+len(tpe1Payload))
	copy(tpe1Frame[:4], "TPE1")
	binary.BigEndian.PutUint32(tpe1Frame[4:8], uint32(len(tpe1Payload)))
	copy(tpe1Frame[10:], tpe1Payload)

	frames := append(tit2Frame, tpe1Frame...)
	tagSize := len(frames)

	header := []byte{'I', 'D', '3', 3, 0, 0,
		byte((tagSize >> 21) & 0x7f),
		byte((tagSize >> 14) & 0x7f),
		byte((tagSize >> 7) & 0x7f),
		byte(tagSize & 0x7f),
	}

	data := append(header, frames...)

	meta := ExtractMetadata(data)
	assert.Equal(t, "Test Song Title", meta.Title)
	assert.Equal(t, "Test Artist", meta.Artist)
}

func TestExtractMetadata_ID3v1(t *testing.T) {
	data := make([]byte, 128)
	copy(data[:3], "TAG")
	copy(data[3:33], "V1 Song Title")
	copy(data[33:63], "V1 Artist Name")

	meta := ExtractMetadata(data)
	assert.Equal(t, "V1 Song Title", meta.Title)
	assert.Equal(t, "V1 Artist Name", meta.Artist)
}

func TestExtractMetadata_Empty(t *testing.T) {
	meta := ExtractMetadata([]byte("invalid random data"))
	assert.Equal(t, "", meta.Title)
	assert.Equal(t, "", meta.Artist)
}

func FuzzExtractMetadata(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("ID3"))
	f.Add([]byte("not audio"))

	// Minimal valid ID3v2.3 header
	f.Add([]byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0})

	// Minimal ID3v1 tag (128 bytes starting with TAG)
	v1 := make([]byte, 128)
	copy(v1[:3], "TAG")
	f.Add(v1)

	f.Fuzz(func(t *testing.T, data []byte) {
		ExtractMetadata(data)
	})
}

