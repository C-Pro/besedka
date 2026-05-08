package assets

import (
	"io"
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
