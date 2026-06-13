package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"besedka/internal/storage"
)

// AttachThumbnail generates a thumbnail for meta's content, saves the blob
// (encrypted by the store like any other blob) and fills the Thumbnail*
// fields of meta in place. It returns false when no thumbnail applies
// (non-image, SVG, small file, unsupported format, or already present).
// It does not persist meta - callers are expected to upsert it.
func AttachThumbnail(store *storage.BboltStorage, meta *storage.FileMetadata, data []byte) (bool, error) {
	if !strings.HasPrefix(meta.MimeType, "image/") ||
		meta.MimeType == "image/svg+xml" ||
		int64(len(data)) <= ThumbnailThreshold ||
		meta.ThumbnailHash != "" {
		return false, nil
	}

	thumb, thumbMime, err := GenerateThumbnail(data, meta.MimeType)
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			return false, nil
		}
		return false, fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	hasher := sha256.New()
	hasher.Write(thumb)
	thumbHash := hex.EncodeToString(hasher.Sum(nil))

	if err := store.SaveFileBlob(bytes.NewReader(thumb), thumbHash); err != nil {
		return false, fmt.Errorf("failed to save thumbnail blob: %w", err)
	}

	meta.ThumbnailHash = thumbHash
	meta.ThumbnailMime = thumbMime
	meta.ThumbnailSize = int64(len(thumb))
	return true, nil
}
