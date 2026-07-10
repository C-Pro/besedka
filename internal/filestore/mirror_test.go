package filestore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"besedka/internal/objectstore"
)

// fakeS3 is a tiny in-memory S3-compatible server supporting PUT/GET/DELETE of
// objects and ListObjectsV2, enough to exercise the real objectstore.Client.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	bucket  string
}

func newFakeS3(bucket string) *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}, bucket: bucket}
}

func (f *fakeS3) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		bucketPrefix := "/" + f.bucket
		// List: GET on the bucket root with list-type=2.
		if r.Method == "GET" && r.URL.Path == bucketPrefix && r.URL.Query().Get("list-type") == "2" {
			prefix := r.URL.Query().Get("prefix")
			var keys []string
			for k := range f.objects {
				if strings.HasPrefix(k, prefix) {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			var b strings.Builder
			b.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
			for _, k := range keys {
				fmt.Fprintf(&b, `<Contents><Key>%s</Key><Size>%d</Size>`+
					`<LastModified>2026-01-01T00:00:00.000Z</LastModified><ETag>"x"</ETag></Contents>`,
					k, len(f.objects[k]))
			}
			b.WriteString(`</ListBucketResult>`)
			_, _ = io.WriteString(w, b.String())
			return
		}

		key := strings.TrimPrefix(r.URL.Path, bucketPrefix+"/")
		switch r.Method {
		case "PUT":
			body, _ := io.ReadAll(r.Body)
			f.objects[key] = body
			w.WriteHeader(http.StatusOK)
		case "GET":
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
				return
			}
			_, _ = w.Write(data)
		case "DELETE":
			delete(f.objects, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func (f *fakeS3) get(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	return b, ok
}

func (f *fakeS3) put(key string, data []byte) {
	f.mu.Lock()
	f.objects[key] = data
	f.mu.Unlock()
}

func newMirrorForTest(t *testing.T, fake *fakeS3) (*MirrorFileStore, *LocalFileStore) {
	t.Helper()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	local, err := NewLocalFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	obj, err := objectstore.New(objectstore.Config{
		Endpoint: srv.URL, Region: "us-east-1", Bucket: fake.bucket,
		AccessKey: "A", SecretKey: "S", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewMirrorFileStore(local, obj, "files/"), local
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func TestMirrorSaveUploadsAsync(t *testing.T) {
	fake := newFakeS3("testbucket")
	m, local := newMirrorForTest(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Start(ctx)

	payload := []byte("file contents")
	if err := m.Save(bytes.NewReader(payload), "deadbeef"); err != nil {
		t.Fatal(err)
	}

	// Local write is synchronous.
	if _, err := local.Get("deadbeef"); err != nil {
		t.Errorf("expected local copy: %v", err)
	}
	// Upload is async; wait for it.
	waitFor(t, func() bool {
		data, ok := fake.get("files/deadbeef")
		return ok && bytes.Equal(data, payload)
	})
}

func TestMirrorGetFallbackAndRecache(t *testing.T) {
	fake := newFakeS3("testbucket")
	m, local := newMirrorForTest(t, fake)

	// Seed object storage only; local is empty.
	payload := []byte("only in s3")
	fake.put("files/cafebabe", payload)

	rc, err := m.Get("cafebabe")
	if err != nil {
		t.Fatalf("expected fallback to S3: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}

	// Should have been re-cached locally.
	lrc, err := local.Get("cafebabe")
	if err != nil {
		t.Fatalf("expected re-cache to local: %v", err)
	}
	_ = lrc.Close()
}

func TestMirrorGetMissingEverywhere(t *testing.T) {
	fake := newFakeS3("testbucket")
	m, _ := newMirrorForTest(t, fake)

	if _, err := m.Get("nope"); err == nil {
		t.Error("expected error when blob missing locally and in S3")
	}
}

func TestMirrorBackfill(t *testing.T) {
	fake := newFakeS3("testbucket")
	m, local := newMirrorForTest(t, fake)

	// Pre-existing local files, not yet in S3.
	for _, h := range []string{"aa11", "bb22", "cc33"} {
		if err := local.Save(bytes.NewReader([]byte("blob-"+h)), h); err != nil {
			t.Fatal(err)
		}
	}
	// One already present in S3 — should not be re-uploaded (still ends present).
	fake.put("files/bb22", []byte("blob-bb22"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Start(ctx)

	waitFor(t, func() bool {
		_, a := fake.get("files/aa11")
		_, c := fake.get("files/cc33")
		return a && c
	})
}

func TestMirrorFlush(t *testing.T) {
	fake := newFakeS3("testbucket")
	m, local := newMirrorForTest(t, fake)
	// Note: no Start — Flush alone must perform the uploads, synchronously.

	for _, h := range []string{"aa11", "bb22", "cc33"} {
		if err := local.Save(bytes.NewReader([]byte("blob-"+h)), h); err != nil {
			t.Fatal(err)
		}
	}
	// Already mirrored: must not be disturbed.
	fake.put("files/bb22", []byte("blob-bb22"))

	if err := m.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"aa11", "bb22", "cc33"} {
		data, ok := fake.get("files/" + h)
		if !ok || !bytes.Equal(data, []byte("blob-"+h)) {
			t.Errorf("blob %s missing or corrupt after flush", h)
		}
	}
}

func TestMirrorFlushSurfacesUploadErrors(t *testing.T) {
	// A backend that lists fine but refuses every PUT.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
	}))
	t.Cleanup(srv.Close)

	local, err := NewLocalFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	obj, err := objectstore.New(objectstore.Config{
		Endpoint: srv.URL, Region: "us-east-1", Bucket: "testbucket",
		AccessKey: "A", SecretKey: "S", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := NewMirrorFileStore(local, obj, "files/")

	if err := local.Save(bytes.NewReader([]byte("blob")), "dead"); err != nil {
		t.Fatal(err)
	}
	if err := m.Flush(context.Background()); err == nil {
		t.Error("expected Flush to surface the upload failure")
	}
}
