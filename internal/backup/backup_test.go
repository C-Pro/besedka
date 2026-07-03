package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"besedka/internal/filestore"
	"besedka/internal/objectstore"
	"besedka/internal/storage"
)

// fakeS3 is a minimal in-memory S3-compatible backend for tests.
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
		}
	})
}

func (f *fakeS3) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			n++
		}
	}
	return n
}

func newClient(t *testing.T, fake *fakeS3) *objectstore.Client {
	t.Helper()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	c, err := objectstore.New(objectstore.Config{
		Endpoint: srv.URL, Region: "us-east-1", Bucket: fake.bucket,
		AccessKey: "A", SecretKey: "S", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// newStorage opens an encrypted BboltStorage at dir/besedka.db with the given
// secret and seeds a config value used to verify recovery.
func newStorage(t *testing.T, dir, secret string) (*storage.BboltStorage, string) {
	t.Helper()
	dbPath := filepath.Join(dir, "besedka.db")
	local, err := filestore.NewLocalFileStore(filepath.Join(dir, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := storage.NewBboltStorage(dbPath, []byte(secret), local)
	if err != nil {
		t.Fatal(err)
	}
	return st, dbPath
}

func TestBackupAndRecover(t *testing.T) {
	const secret = "test-secret"
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)

	st, dbPath := newStorage(t, dir, secret)
	if err := st.SetConfig("greeting", "hello-world"); err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(st, client, "backups/", time.Hour, 7)
	if err := sched.BackupOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.count("backups/") != 1 {
		t.Fatalf("expected 1 backup, got %d", fake.count("backups/"))
	}
	_ = st.Close()

	// Simulate disk loss: remove the db file.
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}

	recovered, err := RecoverDBIfMissing(context.Background(), dbPath, secret, "backups/", client)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered {
		t.Fatal("expected recovery to occur")
	}

	// Reopen and verify the data survived the round-trip.
	st2, _ := newStorage(t, dir, secret)
	defer func() { _ = st2.Close() }()
	got, err := st2.GetConfig("greeting")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello-world" {
		t.Errorf("recovered value = %q, want hello-world", got)
	}

	// Recovered file must be 0600.
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("recovered db perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestBackupArtifactIsEncrypted(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	sched := NewScheduler(st, client, "backups/", time.Hour, 7)
	if err := sched.BackupOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The artifact payload must not contain the raw bbolt magic ("\x00... LCK"
	// or the literal "besedka"-set data). Check it does not start with the
	// bbolt page signature once past our header.
	var artifact []byte
	for k, v := range fake.objects {
		if strings.HasPrefix(k, "backups/") {
			artifact = v
		}
	}
	hdr, payload, err := readHeader(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if len(hdr.salt) == 0 {
		t.Fatal("expected backup header to carry a salt")
	}
	// bbolt files begin with a meta page; encrypted payload should not.
	if bytes.HasPrefix(payload, []byte{0, 0, 0, 0}) {
		t.Error("payload looks unencrypted")
	}
}

func TestBackupIfStale(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	// fakeS3 reports LastModified 2026-01-01T00:00:00Z for every object.
	sched := NewScheduler(st, client, "backups/", time.Hour, 7)
	sched.now = func() time.Time { return time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC) }

	// Empty bucket: backs up immediately.
	if err := sched.backupIfStale(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.count("backups/"); got != 1 {
		t.Fatalf("expected 1 backup, got %d", got)
	}

	// Newest backup is younger than the interval: no new backup.
	if err := sched.backupIfStale(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.count("backups/"); got != 1 {
		t.Fatalf("expected still 1 backup, got %d", got)
	}

	// Past the interval: a new backup is taken.
	sched.now = func() time.Time { return time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC) }
	if err := sched.backupIfStale(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.count("backups/"); got != 2 {
		t.Fatalf("expected 2 backups, got %d", got)
	}
}

func TestRetentionPrunesOldest(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	sched := NewScheduler(st, client, "backups/", time.Hour, 2)

	// Four backups at increasing timestamps; keep=2 => 2 remain (the newest).
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Hour)
		sched.now = func() time.Time { return ts }
		if err := sched.BackupOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if got := fake.count("backups/"); got != 2 {
		t.Fatalf("expected 2 backups after retention, got %d", got)
	}
	// The two newest (03:00 and 02:00) should remain.
	if _, ok := fake.objects["backups/besedka-20260101T030000Z.bak"]; !ok {
		t.Error("newest backup missing")
	}
	if _, ok := fake.objects["backups/besedka-20260101T000000Z.bak"]; ok {
		t.Error("oldest backup should have been pruned")
	}
}

func TestRecoverWrongSecretFails(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, dbPath := newStorage(t, dir, "right-secret")

	sched := NewScheduler(st, client, "backups/", time.Hour, 7)
	if err := sched.BackupOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	_ = os.Remove(dbPath)

	_, err := RecoverDBIfMissing(context.Background(), dbPath, "wrong-secret", "backups/", client)
	if err == nil {
		t.Fatal("expected recovery with wrong secret to fail")
	}
	if _, statErr := os.Stat(dbPath); statErr == nil {
		t.Error("db file should not exist after failed decryption")
	}
}

func TestRecoverNoOpWhenDBExists(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	dbPath := filepath.Join(dir, "besedka.db")
	if err := os.WriteFile(dbPath, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverDBIfMissing(context.Background(), dbPath, "s", "backups/", client)
	if err != nil || recovered {
		t.Errorf("expected no-op, got recovered=%v err=%v", recovered, err)
	}
}

func TestRecoverNoOpWhenNoBackups(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	dbPath := filepath.Join(dir, "besedka.db")
	recovered, err := RecoverDBIfMissing(context.Background(), dbPath, "s", "backups/", client)
	if err != nil || recovered {
		t.Errorf("expected no-op for empty bucket, got recovered=%v err=%v", recovered, err)
	}
}

func TestRecoverDisabledClient(t *testing.T) {
	recovered, err := RecoverDBIfMissing(context.Background(), "/nonexistent/besedka.db", "s", "backups/", nil)
	if err != nil || recovered {
		t.Errorf("expected no-op for nil client, got recovered=%v err=%v", recovered, err)
	}
}
