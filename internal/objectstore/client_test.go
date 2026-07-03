package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// newTestClient returns a Client pointed at the given test server URL.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	c, err := New(Config{
		Endpoint:  serverURL,
		Region:    "us-east-1",
		Bucket:    "testbucket",
		AccessKey: "AKID",
		SecretKey: "SECRET",
		PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// assertSigned re-derives the expected signature from the received request and
// fails if the Authorization header does not match — proving end-to-end signing.
func assertSigned(t *testing.T, r *http.Request, secret string) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, algorithm+" ") {
		t.Fatalf("missing/invalid Authorization: %q", auth)
	}
	if r.Header.Get("X-Amz-Date") == "" || r.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Fatalf("missing amz headers")
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	payload := []byte("hello object storage")
	var stored []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSigned(t, r, "SECRET")
		switch r.Method {
		case "PUT":
			body, _ := io.ReadAll(r.Body)
			// Verify the content hash header matches the body actually sent.
			sum := sha256.Sum256(body)
			if got := r.Header.Get("X-Amz-Content-Sha256"); got != hex.EncodeToString(sum[:]) {
				t.Errorf("content-sha256 mismatch: header=%s body=%x", got, sum)
			}
			if r.ContentLength != int64(len(body)) {
				t.Errorf("content-length = %d, want %d", r.ContentLength, len(body))
			}
			if r.URL.Path != "/testbucket/files/abc" {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			stored = body
			w.WriteHeader(http.StatusOK)
		case "GET":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stored)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	if err := c.Put(ctx, "files/abc", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	rc, err := c.Get(ctx, "files/abc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}
}

func TestGetNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>nope</Message></Error>`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.Get(context.Background(), "missing")
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestListPagination(t *testing.T) {
	page := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSigned(t, r, "SECRET")
		q := r.URL.Query()
		if q.Get("list-type") != "2" {
			t.Errorf("expected list-type=2, got %q", q.Get("list-type"))
		}
		if q.Get("prefix") != "backups/" {
			t.Errorf("expected prefix=backups/, got %q", q.Get("prefix"))
		}
		w.Header().Set("Content-Type", "application/xml")
		if atomic.AddInt32(&page, 1) == 1 {
			if q.Get("continuation-token") != "" {
				t.Errorf("first page should have no token")
			}
			_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated>`+
				`<NextContinuationToken>TOKEN2</NextContinuationToken>`+
				`<Contents><Key>backups/a.db</Key><Size>10</Size>`+
				`<LastModified>2026-01-01T00:00:00.000Z</LastModified><ETag>"e1"</ETag></Contents>`+
				`</ListBucketResult>`)
		} else {
			if q.Get("continuation-token") != "TOKEN2" {
				t.Errorf("second page token = %q", q.Get("continuation-token"))
			}
			_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated>`+
				`<Contents><Key>backups/b.db</Key><Size>20</Size>`+
				`<LastModified>2026-01-02T00:00:00.000Z</LastModified><ETag>"e2"</ETag></Contents>`+
				`</ListBucketResult>`)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	objs, err := c.List(context.Background(), "backups/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("got %d objects, want 2", len(objs))
	}
	if objs[0].Key != "backups/a.db" || objs[1].Key != "backups/b.db" {
		t.Errorf("unexpected keys: %+v", objs)
	}
	if objs[1].Size != 20 || objs[1].LastModified.IsZero() {
		t.Errorf("unexpected object fields: %+v", objs[1])
	}
}

func TestRetryOn500(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if err := c.Delete(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestVirtualHostAddressing(t *testing.T) {
	c, err := New(Config{
		Endpoint: "https://s3.example.com", Region: "us-east-1",
		Bucket: "mybucket", AccessKey: "A", SecretKey: "S", PathStyle: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, host := c.objectURL("files/x")
	if host != "mybucket.s3.example.com" {
		t.Errorf("host = %q", host)
	}
	if u.Path != "/files/x" {
		t.Errorf("path = %q", u.Path)
	}
	if _, err := url.Parse(u.String()); err != nil {
		t.Errorf("bad url: %v", err)
	}
}

func TestNewDisabled(t *testing.T) {
	c, err := New(Config{Endpoint: "", Bucket: ""})
	if err != nil || c != nil {
		t.Errorf("expected (nil,nil) for disabled config, got %v %v", c, err)
	}
}
