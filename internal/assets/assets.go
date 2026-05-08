package assets

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Load returns an fs.FS that substitutes {{CHATNAME}} with chatName in
// .html, .js, and .webmanifest files, delegating all other file reads to originalFS.
func Load(chatName string, originalFS fs.FS) (fs.FS, error) {
	return &overlayFS{original: originalFS, chatName: chatName}, nil
}

type overlayFS struct {
	original fs.FS
	chatName string
}

func (o *overlayFS) Open(name string) (fs.File, error) {
	f, err := o.original.Open(name)
	if err != nil {
		return nil, err
	}

	if !strings.HasSuffix(name, ".html") &&
		!strings.HasSuffix(name, ".js") &&
		!strings.HasSuffix(name, ".webmanifest") {
		return f, nil
	}

	content, err := io.ReadAll(f)
	closeErr := f.Close()

	if err != nil {
		return nil, err
	}

	if closeErr != nil {
		return nil, closeErr
	}

	substituted := bytes.ReplaceAll(content, []byte("{{CHATNAME}}"), []byte(o.chatName))

	return &memFile{
		Reader: bytes.NewReader(substituted),
		name:   path.Base(name),
		size:   int64(len(substituted)),
	}, nil
}

type memFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *memFile) Close() error { return nil }

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: f.size}, nil
}

type memFileInfo struct {
	name string
	size int64
}

func (fi *memFileInfo) Name() string       { return fi.name }
func (fi *memFileInfo) Size() int64        { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (fi *memFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *memFileInfo) IsDir() bool        { return false }
func (fi *memFileInfo) Sys() any           { return nil }

