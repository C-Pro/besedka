package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"besedka/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateSongHandler_ExtractsID3Metadata(t *testing.T) {
	apiInst, as, st, _ := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	_, apiKey, err := as.AddBot("songuser", "Song User", models.BotPermissions{
		Write: true,
	})
	require.NoError(t, err)

	// Create synthetic ID3v2 tagged MP3 data
	titleStr := "ID3 Extract Title"
	artistStr := "ID3 Extract Artist"

	tit2Payload := append([]byte{0}, []byte(titleStr)...)
	tit2Frame := make([]byte, 10+len(tit2Payload))
	copy(tit2Frame[:4], "TIT2")
	binary.BigEndian.PutUint32(tit2Frame[4:8], uint32(len(tit2Payload)))
	copy(tit2Frame[10:], tit2Payload)

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
	audioData := append(header, frames...)

	// Create multipart form with file attached but empty title and artist fields
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "song.mp3")
	require.NoError(t, err)
	_, err = part.Write(audioData)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/api/users/me/song", body)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	handler := apiInst.RequireAuth(apiInst.UpdateSongHandler)
	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		SongTitle  string `json:"songTitle"`
		SongArtist string `json:"songArtist"`
		SongURL    string `json:"songUrl"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "ID3 Extract Title", resp.SongTitle)
	assert.Equal(t, "ID3 Extract Artist", resp.SongArtist)
	assert.NotEmpty(t, resp.SongURL)
}

func TestGetFileHandler_SupportsRangeRequests(t *testing.T) {
	apiInst, as, st, _ := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	_, apiKey, err := as.AddBot("songuser2", "Song User 2", models.BotPermissions{
		Write: true,
	})
	require.NoError(t, err)

	// Upload a file first
	audioData := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.mp3")
	require.NoError(t, err)
	_, err = part.Write(audioData)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/api/users/me/song", body)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	handler := apiInst.RequireAuth(apiInst.UpdateSongHandler)
	handler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		SongURL string `json:"songUrl"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Fetch file with Range header: bytes=10-19
	fileReq := httptest.NewRequest("GET", resp.SongURL, nil)
	fileReq.Header.Set("Authorization", "Bearer "+apiKey)
	fileReq.Header.Set("Range", "bytes=10-19")
	fileReq.SetPathValue("id", resp.SongURL[len("/api/files/"):])

	fileRec := httptest.NewRecorder()
	fileHandler := apiInst.RequireAuth(apiInst.GetFileHandler)
	fileHandler(fileRec, fileReq)

	assert.Equal(t, http.StatusPartialContent, fileRec.Code)
	assert.Equal(t, "bytes 10-19/36", fileRec.Header().Get("Content-Range"))
	assert.Equal(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"[0:10], fileRec.Body.String())
}

func TestUpdateSongHandler_NormalizesMimeType(t *testing.T) {
	apiInst, as, st, _ := setupAPIKeyTest(t)
	defer func() { _ = st.Close() }()

	_, apiKey, err := as.AddBot("songuser3", "Song User 3", models.BotPermissions{
		Write: true,
	})
	require.NoError(t, err)

	// Send audio with non-standard Content-Type: audio/mp3 in multipart form
	mp3Data := append([]byte{0xFF, 0xFB, 0x90, 0x64}, []byte("fake mp3 data payload")...)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="track.mp3"`}
	h["Content-Type"] = []string{"audio/mp3"}
	part, err := writer.CreatePart(h)
	require.NoError(t, err)
	_, err = part.Write(mp3Data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/api/users/me/song", body)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	handler := apiInst.RequireAuth(apiInst.UpdateSongHandler)
	handler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		SongURL string `json:"songUrl"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	rawFileID := resp.SongURL[len("/api/files/"): ]
	fileID := rawFileID
	if idx := strings.LastIndex(fileID, "."); idx != -1 {
		fileID = fileID[:idx]
	}

	// Verify metadata MIME type was normalized to audio/mpeg
	meta, err := st.GetFileMetadata(fileID)
	require.NoError(t, err)
	assert.Equal(t, "audio/mpeg", meta.MimeType)

	// Fetch file via GetFileHandler and verify Content-Type header is audio/mpeg
	fileReq := httptest.NewRequest("GET", resp.SongURL, nil)
	fileReq.Header.Set("Authorization", "Bearer "+apiKey)
	fileReq.SetPathValue("id", rawFileID)

	fileRec := httptest.NewRecorder()
	fileHandler := apiInst.RequireAuth(apiInst.GetFileHandler)
	fileHandler(fileRec, fileReq)

	assert.Equal(t, http.StatusOK, fileRec.Code)
	assert.Equal(t, "audio/mpeg", fileRec.Header().Get("Content-Type"))
}

