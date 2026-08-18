package assets

import (
	"besedka/static"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoad(t *testing.T) {
	adminRaw := "{{.ChatName}} Admin{{range .Users}}{{.Name}}{{end}}"
	mockFS := fstest.MapFS{
		"index.html": {
			Data: []byte("Welcome to {{.ChatName}}"),
		},
		"login.html": {
			Data: []byte("Login to {{.ChatName}}"),
		},
		"config.js": {
			Data: []byte("const app = '{{.ChatName}}';"),
		},
		"site.webmanifest": {
			Data: []byte(`{"name": "{{.ChatName}}"}`),
		},
		"style.css": {
			Data: []byte(".body { color: red; }"),
		},
		"sw.js": {
			Data: []byte("const CACHE_VERSION = '{{.CacheVersion}}';"),
		},
		// admin.html is owned by the admin server; the overlay must serve it raw,
		// leaving its Go template directives (e.g. {{range .Users}}) untouched.
		"admin.html": {
			Data: []byte(adminRaw),
		},
	}

	chatName := "MyCustomChat"
	processedFS, err := Load(chatName, mockFS)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	expectedVersion := compileTime.UTC().Format("20060102150405")
	tests := []struct {
		path     string
		expected string
	}{
		{"index.html", "Welcome to MyCustomChat"},
		{"login.html", "Login to MyCustomChat"},
		{"config.js", "const app = 'MyCustomChat';"},
		{"site.webmanifest", `{"name": "MyCustomChat"}`},
		{"style.css", ".body { color: red; }"},
		{"sw.js", "const CACHE_VERSION = '" + expectedVersion + "';"},
		// Served raw — no substitution applied.
		{"admin.html", adminRaw},
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

func TestLoadMalformedTemplate(t *testing.T) {
	mockFS := fstest.MapFS{
		"broken.html": {
			Data: []byte("oops {{.ChatName"),
		},
	}

	processedFS, _ := Load("TestChat", mockFS)

	if _, err := processedFS.Open("broken.html"); err == nil {
		t.Error("expected error opening malformed template, got nil")
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

func TestSeek(t *testing.T) {
	mockFS := fstest.MapFS{
		"site.webmanifest": {
			Data: []byte(`{"name": "{{.ChatName}}"}`),
		},
		"unmodified.txt": {
			Data: []byte(`hello`),
		},
	}

	processedFS, _ := Load("TestChat", mockFS)

	for _, name := range []string{"site.webmanifest", "unmodified.txt"} {
		file, err := processedFS.Open(name)
		if err != nil {
			t.Fatalf("failed to open %s: %v", name, err)
		}

		seeker, ok := file.(io.Seeker)
		if !ok {
			t.Fatalf("expected file %s to implement io.Seeker", name)
		}

		_, err = seeker.Seek(0, io.SeekEnd)
		if err != nil {
			t.Errorf("seek failed for %s: %v", name, err)
		}

		_ = file.Close()
	}
}

func TestLoadEmbeddedLoginHTML(t *testing.T) {
	processedFS, err := Load("TestBesedkaChat", static.Content)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	file, err := processedFS.Open("login.html")
	if err != nil {
		t.Fatalf("failed to open login.html: %v", err)
	}
	defer file.Close() //nolint:errcheck

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read login.html: %v", err)
	}

	htmlStr := string(content)

	if !strings.Contains(htmlStr, "TestBesedkaChat") {
		t.Errorf("expected login.html to contain rendered chat name TestBesedkaChat")
	}

	if !strings.Contains(htmlStr, `id="login-bg-canvas"`) {
		t.Errorf("expected login.html to contain canvas #login-bg-canvas")
	}

	if !strings.Contains(htmlStr, "https://github.com/C-Pro/besedka") {
		t.Errorf("expected login.html to contain github repo link")
	}

	if !strings.Contains(htmlStr, `name="robots" content="noindex, nofollow, noarchive"`) {
		t.Errorf("expected login.html to contain anti-phishing robots meta tag")
	}
}
