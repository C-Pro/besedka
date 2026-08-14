package audio

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// NormalizeMimeType returns standard IANA MIME type for audio formats if non-standard or alias.
func NormalizeMimeType(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/mp3", "audio/x-mp3", "application/x-mp3", "audio/mpg", "audio/x-mpeg", "audio/mp33", "audio/mp-3":
		return "audio/mpeg"
	case "audio/wave", "audio/x-wav", "audio/vnd.wave":
		return "audio/wav"
	case "audio/x-m4a":
		return "audio/mp4"
	case "audio/x-flac":
		return "audio/flac"
	case "audio/x-aac":
		return "audio/aac"
	case "audio/x-ogg":
		return "audio/ogg"
	default:
		return mimeType
	}
}

// DetectAudioMimeType inspects raw audio bytes to detect standard MIME types.
func DetectAudioMimeType(data []byte) string {
	if len(data) < 3 {
		return ""
	}
	// ID3v2 tag
	if bytes.HasPrefix(data, []byte("ID3")) {
		return "audio/mpeg"
	}
	// MP3 sync frame: 11 bits sync word (0xFF 0xE0..0xFF)
	if data[0] == 0xFF && (data[1]&0xE0) == 0xE0 && (data[1]&0x18) != 0x08 {
		return "audio/mpeg"
	}
	// WAV: "RIFF" ... "WAVE"
	if len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")) {
		return "audio/wav"
	}
	// OGG: "OggS"
	if bytes.HasPrefix(data, []byte("OggS")) {
		return "audio/ogg"
	}
	// FLAC: "fLaC"
	if bytes.HasPrefix(data, []byte("fLaC")) {
		return "audio/flac"
	}
	// MP4 / M4A: "ftyp" at index 4
	if len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) {
		return "audio/mp4"
	}
	// ID3v1 tag at end of file
	if len(data) >= 128 && bytes.Equal(data[len(data)-128:len(data)-125], []byte("TAG")) {
		return "audio/mpeg"
	}
	return ""
}


// Metadata represents audio track metadata extracted from tags.
type Metadata struct {
	Title  string
	Artist string
}

// ExtractMetadata parses ID3v2, ID3v1, or MP4 metadata from raw audio file bytes.
func ExtractMetadata(data []byte) Metadata {
	meta := parseID3v2(data)
	if meta.Title == "" || meta.Artist == "" {
		v1 := parseID3v1(data)
		if meta.Title == "" {
			meta.Title = v1.Title
		}
		if meta.Artist == "" {
			meta.Artist = v1.Artist
		}
	}
	if meta.Title == "" || meta.Artist == "" {
		mp4Meta := parseMP4Meta(data)
		if meta.Title == "" {
			meta.Title = mp4Meta.Title
		}
		if meta.Artist == "" {
			meta.Artist = mp4Meta.Artist
		}
	}
	return meta
}

func parseID3v2(data []byte) Metadata {
	var meta Metadata
	if len(data) < 10 {
		return meta
	}

	if !bytes.Equal(data[:3], []byte("ID3")) {
		return meta
	}

	version := data[3]
	if version < 2 || version > 4 {
		return meta
	}

	flags := data[5]
	hasExtendedHeader := (flags & 0x40) != 0

	tagSize := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
	if 10+tagSize > len(data) {
		tagSize = len(data) - 10
	}

	offset := 10
	if hasExtendedHeader && offset+4 <= len(data) {
		extSize := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4 + extSize
	}

	limit := 10 + tagSize
	for offset+10 <= limit {
		frameID := string(data[offset : offset+4])
		if frameID[0] == 0 {
			break
		}

		var frameSize int
		if version == 4 {
			frameSize = int(data[offset+4])<<21 | int(data[offset+5])<<14 | int(data[offset+6])<<7 | int(data[offset+7])
		} else {
			frameSize = int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		}

		offset += 10
		if frameSize <= 0 || offset+frameSize > limit {
			break
		}

		frameData := data[offset : offset+frameSize]
		offset += frameSize

		switch frameID {
		case "TIT2", "TT2":
			meta.Title = decodeTextFrame(frameData)
		case "TPE1", "TP1":
			meta.Artist = decodeTextFrame(frameData)
		}
	}

	return meta
}

func decodeTextFrame(b []byte) string {
	if len(b) < 2 {
		if len(b) == 1 {
			return ""
		}
		return string(bytes.TrimRight(b, "\x00"))
	}
	encoding := b[0]
	payload := b[1:]

	var text string
	switch encoding {
	case 0:
		text = string(bytes.TrimRight(payload, "\x00"))
	case 1:
		text = decodeUTF16(payload, true)
	case 2:
		text = decodeUTF16BE(payload)
	case 3:
		text = string(bytes.TrimRight(payload, "\x00"))
	default:
		text = string(bytes.TrimRight(payload, "\x00"))
	}
	return text
}

func decodeUTF16(b []byte, checkBOM bool) string {
	if len(b) < 2 {
		return ""
	}
	bigEndian := true
	if checkBOM {
		if b[0] == 0xFF && b[1] == 0xFE {
			bigEndian = false
			b = b[2:]
		} else if b[0] == 0xFE && b[1] == 0xFF {
			bigEndian = true
			b = b[2:]
		}
	}

	u16 := make([]uint16, len(b)/2)
	for i := 0; i < len(u16); i++ {
		if bigEndian {
			u16[i] = binary.BigEndian.Uint16(b[i*2:])
		} else {
			u16[i] = binary.LittleEndian.Uint16(b[i*2:])
		}
	}
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16.Decode(u16))
}

func decodeUTF16BE(b []byte) string {
	return decodeUTF16(b, false)
}

func parseID3v1(data []byte) Metadata {
	var meta Metadata
	if len(data) < 128 {
		return meta
	}
	v1Data := data[len(data)-128:]
	if !bytes.Equal(v1Data[:3], []byte("TAG")) {
		return meta
	}
	meta.Title = cleanString(v1Data[3:33])
	meta.Artist = cleanString(v1Data[33:63])
	return meta
}

func cleanString(b []byte) string {
	n := bytes.IndexByte(b, 0)
	if n >= 0 {
		b = b[:n]
	}
	return string(bytes.TrimSpace(b))
}

func parseMP4Meta(data []byte) Metadata {
	var meta Metadata
	titleIdx := bytes.Index(data, []byte("\xa9nam"))
	if titleIdx != -1 && titleIdx+16 < len(data) {
		meta.Title = extractMP4DataAtom(data[titleIdx:])
	}
	artistIdx := bytes.Index(data, []byte("\xa9ART"))
	if artistIdx != -1 && artistIdx+16 < len(data) {
		meta.Artist = extractMP4DataAtom(data[artistIdx:])
	}
	return meta
}

func extractMP4DataAtom(b []byte) string {
	dataIdx := bytes.Index(b, []byte("data"))
	if dataIdx == -1 || dataIdx+16 > len(b) {
		return ""
	}
	content := b[dataIdx+12:]
	end := bytes.IndexByte(content, 0)
	if end != -1 {
		content = content[:end]
	}
	if len(content) > 256 {
		content = content[:256]
	}
	return string(bytes.TrimSpace(content))
}
