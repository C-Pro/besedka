package assets

import (
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing/fstest"
)

// Load reads all files from the provided fs.FS, performs placeholder substitution
// for {{CHATNAME}} in relevant files, and returns an in-memory filesystem.
func Load(chatName string, originalFS fs.FS) (fs.FS, error) {
	mapFS := fstest.MapFS{}

	err := fs.WalkDir(originalFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		file, err := originalFS.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}
		defer func() { _ = file.Close() }()

		content, err := io.ReadAll(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		// Perform substitution for relevant file types
		if strings.HasSuffix(path, ".html") ||
			strings.HasSuffix(path, ".js") ||
			strings.HasSuffix(path, ".webmanifest") {
			contentStr := strings.ReplaceAll(string(content), "{{CHATNAME}}", chatName)
			content = []byte(contentStr)
		}

		mapFS[path] = &fstest.MapFile{
			Data: content,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return mapFS, nil
}
