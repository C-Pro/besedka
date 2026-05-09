package assets

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"runtime/debug"
	"strings"
	"time"
)

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

	// Only substitute .html, .js, .css and .webmanifest files
	if !stats.IsDir() && (strings.HasSuffix(name, ".html") ||
		strings.HasSuffix(name, ".js") ||
		strings.HasSuffix(name, ".css") ||
		strings.HasSuffix(name, ".webmanifest")) {

		content, err := io.ReadAll(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		content = bytes.ReplaceAll(content, []byte("{{CHATNAME}}"), []byte(o.chatName))
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

func (fi *memFileInfo) Name() string       { return fi.name }
func (fi *memFileInfo) Size() int64        { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode  {
	if fi.isDir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (fi *memFileInfo) ModTime() time.Time { return compileTime }
func (fi *memFileInfo) IsDir() bool        { return fi.isDir }
func (fi *memFileInfo) Sys() any           { return nil }
