package assets

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestLoad(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": {
			Data: []byte("Welcome to {{CHATNAME}}"),
		},
		"config.js": {
			Data: []byte("const app = '{{CHATNAME}}';"),
		},
		"site.webmanifest": {
			Data: []byte(`{"name": "{{CHATNAME}}"}`),
		},
		"style.css": {
			Data: []byte(".body { color: red; }"),
		},
	}

	chatName := "MyCustomChat"
	processedFS, err := Load(chatName, mockFS)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	tests := []struct {
		path     string
		expected string
	}{
		{"index.html", "Welcome to MyCustomChat"},
		{"config.js", "const app = 'MyCustomChat';"},
		{"site.webmanifest", `{"name": "MyCustomChat"}`},
		{"style.css", ".body { color: red; }"},
	}

	for _, tt := range tests {
		file, err := processedFS.Open(tt.path)
		if err != nil {
			t.Errorf("failed to open %s: %v", tt.path, err)
			continue
		}
		content, _ := io.ReadAll(file)
		if string(content) != tt.expected {
			t.Errorf("for %s, expected %q, got %q", tt.path, tt.expected, string(content))
		}
		_ = file.Close()
	}
}

func TestDirectoryListing(t *testing.T) {
	mockFS := fstest.MapFS{
		"dir/file.txt": {Data: []byte("hello")},
	}

	processedFS, _ := Load("TestChat", mockFS)

	// Open the directory
	file, err := processedFS.Open("dir")
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer file.Close() //nolint:errcheck

	// Check if it's a directory
	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("failed to stat dir: %v", err)
	}

	if !stat.IsDir() {
		t.Errorf("expected dir to be a directory")
	}

	// Try to read the directory
	dirFile, ok := file.(fs.ReadDirFile)
	if !ok {
		t.Fatal("expected file to be fs.ReadDirFile")
	}

	entries, err := dirFile.ReadDir(-1)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	if len(entries) != 1 || entries[0].Name() != "file.txt" {
		t.Errorf("unexpected directory entries: %+v", entries)
	}
}
