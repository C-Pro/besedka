package images

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"besedka/internal/storage"
)

// migrationConfigKey marks the thumbnail backfill as done in the settings bucket.
const migrationConfigKey = "imageThumbnails"

// avatarURLPattern matches locally served avatar URLs that have no query
// string yet, leaving external URLs and already rewritten ones alone.
var avatarURLPattern = regexp.MustCompile(`^/api/images/[^?]+$`)

// EnsureThumbnails backfills thumbnails for all existing image files and
// rewrites stored user avatar URLs to request thumbnails. It is blocking and
// must run after storage initialization, before the HTTP servers start.
// A repeated run is a no-op thanks to the settings key; an interrupted run
// resumes where it left off because files with thumbnails are skipped.
func EnsureThumbnails(store *storage.BboltStorage) error {
	done, err := store.GetConfig(migrationConfigKey)
	if err != nil {
		return fmt.Errorf("failed to read %s config: %w", migrationConfigKey, err)
	}
	if done == "1" {
		return nil
	}

	metas, err := store.ListFileMetadata()
	if err != nil {
		return fmt.Errorf("failed to list file metadata: %w", err)
	}

	slog.Info("running thumbnail migration", "files", len(metas))

	var generated, failed int
	for i, meta := range metas {
		ok, err := backfillThumbnail(store, meta)
		if err != nil {
			failed++
			slog.Warn("thumbnail migration: failed to process file, skipping", "fileID", meta.ID, "error", err)
		} else if ok {
			generated++
		}

		if (i+1)%50 == 0 {
			slog.Info("thumbnail migration progress", "processed", i+1, "total", len(metas))
		}
	}

	if err := rewriteAvatarURLs(store); err != nil {
		return err
	}

	if err := store.SetConfig(migrationConfigKey, "1"); err != nil {
		return fmt.Errorf("failed to persist %s config: %w", migrationConfigKey, err)
	}

	slog.Info("thumbnail migration done",
		"generated", generated,
		"skipped", len(metas)-generated-failed,
		"failed", failed,
	)
	return nil
}

// backfillThumbnail generates and persists a thumbnail for a single file.
// It reports whether a thumbnail was generated.
func backfillThumbnail(store *storage.BboltStorage, meta storage.FileMetadata) (bool, error) {
	// Cheap skip before touching the blob; AttachThumbnail rechecks.
	if meta.ThumbnailHash != "" ||
		meta.Size <= ThumbnailThreshold ||
		!strings.HasPrefix(meta.MimeType, "image/") ||
		meta.MimeType == "image/svg+xml" {
		return false, nil
	}

	rc, err := store.GetFileBlob(meta.Hash)
	if err != nil {
		return false, fmt.Errorf("failed to get file blob: %w", err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return false, fmt.Errorf("failed to read file blob: %w", err)
	}

	ok, err := AttachThumbnail(store, &meta, data)
	if err != nil || !ok {
		return false, err
	}

	if err := store.UpsertFileMetadata(meta); err != nil {
		return false, fmt.Errorf("failed to update file metadata: %w", err)
	}
	return true, nil
}

// rewriteAvatarURLs appends ?thumb=1 to locally served avatar URLs so all
// clients load avatar thumbnails. Serving falls back to the original for
// files without a thumbnail, so the rewrite is safe for SVG and small images.
func rewriteAvatarURLs(store *storage.BboltStorage) error {
	credentials, err := store.ListAllCredentials()
	if err != nil {
		return fmt.Errorf("failed to list credentials: %w", err)
	}

	for _, creds := range credentials {
		if !avatarURLPattern.MatchString(creds.AvatarURL) {
			continue
		}
		creds.AvatarURL += "?thumb=1"
		if err := store.UpsertCredentials(creds); err != nil {
			return fmt.Errorf("failed to update avatar URL for user %s: %w", creds.ID, err)
		}
	}
	return nil
}
