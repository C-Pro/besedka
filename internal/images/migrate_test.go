package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"io"
	"path/filepath"
	"testing"

	"besedka/internal/auth"
	"besedka/internal/filestore"
	"besedka/internal/models"
	"besedka/internal/storage"
)

func newTestStore(t *testing.T) *storage.BboltStorage {
	t.Helper()
	tmpDir := t.TempDir()
	fs, err := filestore.NewLocalFileStore(filepath.Join(tmpDir, "fs"))
	if err != nil {
		t.Fatalf("failed to create filestore: %v", err)
	}
	store, err := storage.NewBboltStorage(filepath.Join(tmpDir, "test.db"), []byte("secret"), fs)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedFile(t *testing.T, store *storage.BboltStorage, id, mime string, data []byte) storage.FileMetadata {
	t.Helper()
	hasher := sha256.New()
	hasher.Write(data)
	hash := hex.EncodeToString(hasher.Sum(nil))

	if err := store.SaveFileBlob(bytes.NewReader(data), hash); err != nil {
		t.Fatalf("failed to save blob: %v", err)
	}
	meta := storage.FileMetadata{
		ID:       id,
		Hash:     hash,
		MimeType: mime,
		Size:     int64(len(data)),
	}
	if err := store.UpsertFileMetadata(meta); err != nil {
		t.Fatalf("failed to save metadata: %v", err)
	}
	return meta
}

func TestEnsureThumbnails(t *testing.T) {
	store := newTestStore(t)

	bigPNG := encodePNG(t, noiseImage(t, 1200, 900, 255))
	if len(bigPNG) <= ThumbnailThreshold {
		t.Fatalf("test image too small: %d bytes", len(bigPNG))
	}
	smallPNG := encodePNG(t, noiseImage(t, 50, 50, 255))
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	garbage := bytes.Repeat([]byte("not webp at all "), 10*1024)

	seedFile(t, store, "big", "image/png", bigPNG)
	seedFile(t, store, "small", "image/png", smallPNG)
	seedFile(t, store, "svg", "image/svg+xml", svg)
	seedFile(t, store, "webp", "image/webp", garbage)

	if err := EnsureThumbnails(store); err != nil {
		t.Fatalf("EnsureThumbnails failed: %v", err)
	}

	big, err := store.GetFileMetadata("big")
	if err != nil {
		t.Fatalf("GetFileMetadata failed: %v", err)
	}
	if big.ThumbnailHash == "" {
		t.Fatal("expected thumbnail for big image")
	}
	if big.ThumbnailMime != "image/jpeg" {
		t.Errorf("expected image/jpeg thumbnail, got %s", big.ThumbnailMime)
	}
	rc, err := store.GetFileBlob(big.ThumbnailHash)
	if err != nil {
		t.Fatalf("failed to get thumbnail blob: %v", err)
	}
	thumb, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("failed to read thumbnail blob: %v", err)
	}
	if int64(len(thumb)) != big.ThumbnailSize {
		t.Errorf("thumbnail size mismatch: blob %d, metadata %d", len(thumb), big.ThumbnailSize)
	}

	for _, id := range []string{"small", "svg", "webp"} {
		meta, err := store.GetFileMetadata(id)
		if err != nil {
			t.Fatalf("GetFileMetadata %s failed: %v", id, err)
		}
		if meta.ThumbnailHash != "" {
			t.Errorf("unexpected thumbnail for %s", id)
		}
	}

	done, err := store.GetConfig("imageThumbnails")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if done != currentMigrationVersion {
		t.Errorf("expected imageThumbnails=%s, got %q", currentMigrationVersion, done)
	}

	// Second run must be a no-op.
	if err := EnsureThumbnails(store); err != nil {
		t.Fatalf("repeated EnsureThumbnails failed: %v", err)
	}
}

func TestEnsureThumbnailsResume(t *testing.T) {
	store := newTestStore(t)

	bigPNG := encodePNG(t, noiseImage(t, 1200, 900, 255))
	meta := seedFile(t, store, "done", "image/png", bigPNG)

	// Simulate a file processed before a crash: thumbnail fields already set.
	meta.ThumbnailHash = "preexisting"
	meta.ThumbnailMime = "image/jpeg"
	meta.ThumbnailSize = 1
	if err := store.UpsertFileMetadata(meta); err != nil {
		t.Fatalf("failed to update metadata: %v", err)
	}

	if err := EnsureThumbnails(store); err != nil {
		t.Fatalf("EnsureThumbnails failed: %v", err)
	}

	got, err := store.GetFileMetadata("done")
	if err != nil {
		t.Fatalf("GetFileMetadata failed: %v", err)
	}
	if got.ThumbnailHash != "preexisting" {
		t.Errorf("expected preexisting thumbnail to be kept, got %s", got.ThumbnailHash)
	}
}

// TestEnsureThumbnailsRegeneratesOriented simulates upgrading from the
// orientation-unaware version "1": a file whose original carries an EXIF
// orientation must have its thumbnail rebuilt, while a correctly oriented one
// must be left untouched.
func TestEnsureThumbnailsRegeneratesOriented(t *testing.T) {
	store := newTestStore(t)

	rotated := jpegWithOrientation(t, noiseImage(t, 1000, 600, 255), 6)
	upright := jpegWithOrientation(t, noiseImage(t, 1000, 600, 255), 1)
	if len(rotated) <= ThumbnailThreshold || len(upright) <= ThumbnailThreshold {
		t.Fatalf("test images too small: rotated=%d upright=%d", len(rotated), len(upright))
	}

	// Seed both files with a placeholder thumbnail as version "1" would leave them.
	for _, f := range []struct {
		id   string
		data []byte
	}{{"rotated", rotated}, {"upright", upright}} {
		meta := seedFile(t, store, f.id, "image/jpeg", f.data)
		meta.ThumbnailHash = "v1-" + f.id
		meta.ThumbnailMime = "image/jpeg"
		meta.ThumbnailSize = 1
		if err := store.UpsertFileMetadata(meta); err != nil {
			t.Fatalf("failed to seed thumbnail: %v", err)
		}
	}
	if err := store.SetConfig("imageThumbnails", "1"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	if err := EnsureThumbnails(store); err != nil {
		t.Fatalf("EnsureThumbnails failed: %v", err)
	}

	// The oriented file was regenerated into a real, portrait thumbnail.
	rotatedMeta, err := store.GetFileMetadata("rotated")
	if err != nil {
		t.Fatalf("GetFileMetadata failed: %v", err)
	}
	if rotatedMeta.ThumbnailHash == "v1-rotated" || rotatedMeta.ThumbnailHash == "" {
		t.Fatalf("expected oriented thumbnail to be regenerated, got %q", rotatedMeta.ThumbnailHash)
	}
	rc, err := store.GetFileBlob(rotatedMeta.ThumbnailHash)
	if err != nil {
		t.Fatalf("failed to get regenerated thumbnail: %v", err)
	}
	thumb, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("failed to read thumbnail: %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() >= b.Dy() {
		t.Errorf("expected portrait regenerated thumbnail, got %dx%d", b.Dx(), b.Dy())
	}

	// The correctly oriented file keeps its existing thumbnail.
	uprightMeta, err := store.GetFileMetadata("upright")
	if err != nil {
		t.Fatalf("GetFileMetadata failed: %v", err)
	}
	if uprightMeta.ThumbnailHash != "v1-upright" {
		t.Errorf("expected upright thumbnail to be kept, got %q", uprightMeta.ThumbnailHash)
	}
}

func TestEnsureThumbnailsRewritesAvatarURLs(t *testing.T) {
	store := newTestStore(t)

	users := []auth.UserCredentials{
		{
			User: models.User{
				ID:        "local",
				UserName:  "local",
				AvatarURL: "/api/images/some-file-id",
				Status:    models.UserStatusActive,
			},
			PasswordHash: "hash-local",
		},
		{
			User: models.User{
				ID:        "external",
				UserName:  "external",
				AvatarURL: "https://example.com/avatar.png",
				Status:    models.UserStatusActive,
			},
			PasswordHash: "hash-external",
		},
		{
			User: models.User{
				ID:        "rewritten",
				UserName:  "rewritten",
				AvatarURL: "/api/images/other-id?thumb=1",
				Status:    models.UserStatusActive,
			},
			PasswordHash: "hash-rewritten",
		},
	}
	for _, u := range users {
		if err := store.UpsertCredentials(u); err != nil {
			t.Fatalf("UpsertCredentials failed: %v", err)
		}
	}

	if err := EnsureThumbnails(store); err != nil {
		t.Fatalf("EnsureThumbnails failed: %v", err)
	}

	all, err := store.ListAllCredentials()
	if err != nil {
		t.Fatalf("ListAllCredentials failed: %v", err)
	}
	got := make(map[string]auth.UserCredentials, len(all))
	for _, c := range all {
		got[c.ID] = c
	}

	if url := got["local"].AvatarURL; url != "/api/images/some-file-id?thumb=1" {
		t.Errorf("expected local avatar URL rewritten, got %s", url)
	}
	if url := got["external"].AvatarURL; url != "https://example.com/avatar.png" {
		t.Errorf("expected external avatar URL untouched, got %s", url)
	}
	if url := got["rewritten"].AvatarURL; url != "/api/images/other-id?thumb=1" {
		t.Errorf("expected already rewritten avatar URL untouched, got %s", url)
	}
	if hash := got["local"].PasswordHash; hash != "hash-local" {
		t.Errorf("expected password hash to survive rewrite, got %s", hash)
	}
}
