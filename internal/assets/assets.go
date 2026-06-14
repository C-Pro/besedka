package assets

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"runtime/debug"
	"strings"
	// text/template (not html/template) is intentional: these static files include
	// .js/.css/.webmanifest where HTML escaping would corrupt output, and the only
	// substituted value, ChatName, is validated to ^[a-zA-Z0-9-]{3,32}$ in config.go,
	// so no HTML metacharacters can reach the output.
	"text/template" // nosemgrep: go.lang.security.audit.xss.import-text-template.import-text-template
	"time"
)

// vars holds the values substituted into static template files.
type vars struct {
	ChatName     string
	CacheVersion string
}

var compileTime = func() time.Time {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.time" {
				t, err := time.Parse(time.RFC3339, setting.Value)
				if err == nil {
					return t
				}
			}
		}
	}
	return time.Now()
}()

// Load returns an fs.FS that renders .html, .js, .css, and .webmanifest files as
// text/template templates, substituting {{.ChatName}} and {{.CacheVersion}}, and
// delegating all other file reads to originalFS. admin.html is excluded: it is a full
// Go template owned by the admin server (see internal/http/adminServer.go) and is served
// raw here.
func Load(chatName string, originalFS fs.FS) (fs.FS, error) {
	cacheVersion := compileTime.UTC().Format("20060102150405")
	return &overlayFS{original: originalFS, chatName: chatName, cacheVersion: cacheVersion}, nil
}

type overlayFS struct {
	original     fs.FS
	chatName     string
	cacheVersion string
}

func (o *overlayFS) Open(name string) (fs.File, error) {
	f, err := o.original.Open(name)
	if err != nil {
		return nil, err
	}

	stats, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	mf := &memFile{
		Reader: f,
		name:   path.Base(name),
		size:   stats.Size(),
		isDir:  stats.IsDir(),
		close:  f.Close,
		f:      f,
	}

	// Render .html, .js, .css and .webmanifest files as templates. admin.html is
	// excluded: it is a full Go template owned by the admin server and is served raw.
	if !stats.IsDir() && path.Base(name) != "admin.html" &&
		(strings.HasSuffix(name, ".html") ||
			strings.HasSuffix(name, ".js") ||
			strings.HasSuffix(name, ".css") ||
			strings.HasSuffix(name, ".webmanifest")) {

		content, err := io.ReadAll(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}

		tmpl, err := template.New(name).Parse(string(content))
		if err != nil {
			_ = f.Close()
			return nil, err
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, vars{ChatName: o.chatName, CacheVersion: o.cacheVersion}); err != nil {
			_ = f.Close()
			return nil, err
		}

		content = buf.Bytes()
		mf.Reader = bytes.NewReader(content)
		mf.size = int64(len(content))
	}

	return mf, nil
}

type memFile struct {
	io.Reader
	name  string
	size  int64
	isDir bool
	close func() error
	f     fs.File
}

func (f *memFile) Close() error {
	if f.close == nil {
		return nil
	}

	return f.close()
}

func (f *memFile) Seek(offset int64, whence int) (int64, error) {
	if s, ok := f.Reader.(io.Seeker); ok {
		return s.Seek(offset, whence)
	}
	return 0, errors.New("seeker can't seek")
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: f.size, isDir: f.isDir}, nil
}

func (f *memFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if rdf, ok := f.f.(fs.ReadDirFile); ok {
		return rdf.ReadDir(n)
	}
	return nil, &fs.PathError{Op: "readdir", Path: f.name, Err: fs.ErrInvalid}
}

type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (fi *memFileInfo) Name() string { return fi.name }
func (fi *memFileInfo) Size() int64  { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode {
	if fi.isDir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (fi *memFileInfo) ModTime() time.Time { return compileTime }
func (fi *memFileInfo) IsDir() bool        { return fi.isDir }
func (fi *memFileInfo) Sys() any           { return nil }
