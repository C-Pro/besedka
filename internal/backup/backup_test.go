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
	"besedka/internal/models"
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

	sched := NewScheduler(st, client, "backups/", time.Hour, 0, 7, nil)
	if err := sched.DoBackup(context.Background()); err != nil {
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

	sched := NewScheduler(st, client, "backups/", time.Hour, 0, 7, nil)
	if err := sched.DoBackup(context.Background()); err != nil {
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



func TestRetentionPrunesOldest(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	sched := NewScheduler(st, client, "backups/", time.Hour, 0, 2, nil)

	// Four backups at increasing timestamps; keep=2 => 2 remain (the newest).
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Hour)
		sched.now = func() time.Time { return ts }
		if err := sched.DoBackup(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if got := fake.count("backups/"); got != 2 {
		t.Fatalf("expected 2 backups after retention, got %d", got)
	}
	// The two newest (03:00 and 02:00) should remain.
	if _, ok := fake.objects["backups/besedka-20260101T030000Z-full.bak"]; !ok {
		t.Error("newest backup missing")
	}
	if _, ok := fake.objects["backups/besedka-20260101T000000Z-full.bak"]; ok {
		t.Error("oldest backup should have been pruned")
	}
}

func TestRecoverWrongSecretFails(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, dbPath := newStorage(t, dir, "right-secret")

	sched := NewScheduler(st, client, "backups/", time.Hour, 0, 7, nil)
	if err := sched.DoBackup(context.Background()); err != nil {
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

// seedChat creates a chat and a message so incremental tests have real,
// encrypted, nested-bucket data to ship.
func seedChat(t *testing.T, st *storage.BboltStorage, chatID string, seq int64, content string) {
	t.Helper()
	if err := st.UpsertChat(models.Chat{ID: chatID, Name: "general"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMessage(models.Message{ChatID: chatID, Seq: seq, UserID: "u1", Content: content}); err != nil {
		t.Fatal(err)
	}
}

func TestIncrementalBackupAndRecover(t *testing.T) {
	const secret = "test-secret"
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)

	st, dbPath := newStorage(t, dir, secret)
	seedChat(t, st, "c1", 1, "hello")
	if err := st.SetConfig("greeting", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertToken("u1", "tok-a"); err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 7, nil)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched.now = func() time.Time { return base }
	if err := sched.DoBackup(context.Background()); err != nil {
		t.Fatal(err)
	}

	// First delta: new message, token rotation, config change.
	if err := st.UpsertMessage(models.Message{ChatID: "c1", Seq: 2, UserID: "u1", Content: "world"}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteToken("tok-a"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertToken("u1", "tok-b"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetConfig("greeting", "v2"); err != nil {
		t.Fatal(err)
	}
	sched.now = func() time.Time { return base.Add(10 * time.Minute) }
	if err := sched.DoIncrementalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["backups/besedka-20260101T001000Z-incr.bak"]; !ok {
		t.Fatal("expected an incremental artifact")
	}

	// Second delta.
	if err := st.UpsertMessage(models.Message{ChatID: "c1", Seq: 3, UserID: "u1", Content: "again"}); err != nil {
		t.Fatal(err)
	}
	sched.now = func() time.Time { return base.Add(20 * time.Minute) }
	if err := sched.DoIncrementalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}

	_ = st.Close()
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

	st2, _ := newStorage(t, dir, secret)
	defer func() { _ = st2.Close() }()

	msgs, err := st2.ListMessages("c1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var contents []string
	for _, m := range msgs {
		contents = append(contents, m.Content)
	}
	if len(msgs) != 3 || contents[0] != "hello" || contents[1] != "world" || contents[2] != "again" {
		t.Errorf("recovered messages = %v, want [hello world again]", contents)
	}

	tokens, err := st2.ListTokens()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tokens["tok-a"]; ok {
		t.Error("deleted token tok-a should not survive recovery")
	}
	if tokens["tok-b"] != "u1" {
		t.Error("token tok-b should survive recovery")
	}

	if got, _ := st2.GetConfig("greeting"); got != "v2" {
		t.Errorf("recovered greeting = %q, want v2", got)
	}
}

func TestIncrementalPromotesToFullWhenNoBackups(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 7, nil)
	if err := sched.DoIncrementalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.count("backups/"); got != 1 {
		t.Fatalf("expected 1 backup, got %d", got)
	}
	for k := range fake.objects {
		if !strings.HasSuffix(k, fullSuffix) {
			t.Errorf("first backup should be full, got %s", k)
		}
	}
}

func TestIncrementalPromotesToFullOnDivergence(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 7, nil)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched.now = func() time.Time { return base }
	if err := sched.DoBackup(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Another writer (or a lost chain-state commit) put a newer artifact.
	fake.objects["backups/besedka-20260101T000500Z-incr.bak"] = []byte("foreign")

	sched.now = func() time.Time { return base.Add(10 * time.Minute) }
	if err := sched.DoIncrementalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	fulls := 0
	for k := range fake.objects {
		if strings.HasSuffix(k, fullSuffix) {
			fulls++
		}
	}
	if fulls != 2 {
		t.Errorf("divergence should promote to a full backup: want 2 fulls, got %d", fulls)
	}
}

func TestBrokenChainFailsRecovery(t *testing.T) {
	const secret = "test-secret"
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, dbPath := newStorage(t, dir, secret)
	seedChat(t, st, "c1", 1, "hello")

	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 7, nil)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched.now = func() time.Time { return base }
	if err := sched.DoBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i, content := range []string{"world", "again"} {
		if err := st.UpsertMessage(models.Message{ChatID: "c1", Seq: int64(i + 2), UserID: "u1", Content: content}); err != nil {
			t.Fatal(err)
		}
		delay := time.Duration(i+1) * 10 * time.Minute
		sched.now = func() time.Time { return base.Add(delay) }
		if err := sched.DoIncrementalBackup(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}

	// Delete the middle incremental: the chain has a gap.
	delete(fake.objects, "backups/besedka-20260101T001000Z-incr.bak")

	if _, err := RecoverDBIfMissing(context.Background(), dbPath, secret, "backups/", client); err == nil {
		t.Fatal("expected recovery to fail on a broken chain")
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Error("db file must not exist after a failed chain recovery")
	}
	if _, err := os.Stat(dbPath + ".recover"); err == nil {
		t.Error("temp recovery file must be cleaned up")
	}
}

// TestLegacyV1ArtifactRestores ensures backups taken before incremental
// support ("...Z.bak", header v1) are still treated as full snapshots.
func TestLegacyV1ArtifactRestores(t *testing.T) {
	const secret = "test-secret"
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, dbPath := newStorage(t, dir, secret)
	if err := st.SetConfig("greeting", "legacy"); err != nil {
		t.Fatal(err)
	}

	var snap bytes.Buffer
	if _, err := st.SnapshotTo(&snap); err != nil {
		t.Fatal(err)
	}
	enc, salt, err := st.EncryptBackup(snap.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var artifact bytes.Buffer
	if err := writeHeader(&artifact, header{salt: salt}, enc); err != nil {
		t.Fatal(err)
	}
	fake.objects["backups/besedka-20260101T000000Z.bak"] = artifact.Bytes()

	_ = st.Close()
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}

	recovered, err := RecoverDBIfMissing(context.Background(), dbPath, secret, "backups/", client)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered {
		t.Fatal("expected recovery from legacy artifact")
	}
	st2, _ := newStorage(t, dir, secret)
	defer func() { _ = st2.Close() }()
	if got, _ := st2.GetConfig("greeting"); got != "legacy" {
		t.Errorf("recovered greeting = %q, want legacy", got)
	}
}

// TestPruneKeepsLatestIncrementalsOnly: retention counts full backups, and
// incrementals are kept only for the latest full backup.
func TestPruneKeepsLatestIncrementalsOnly(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	keys := []string{
		"backups/besedka-20260101T000000Z-full.bak",
		"backups/besedka-20260101T001000Z-incr.bak",
		"backups/besedka-20260101T002000Z-incr.bak",
		"backups/besedka-20260102T000000Z.bak", // legacy key counts as full
		"backups/besedka-20260102T001000Z-incr.bak",
		"backups/besedka-20260103T000000Z-full.bak",
		"backups/besedka-20260103T001000Z-incr.bak",
	}
	for _, k := range keys {
		fake.objects[k] = []byte("x")
	}

	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 2, nil)
	if err := sched.prune(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"backups/besedka-20260102T000000Z.bak":      true,
		"backups/besedka-20260103T000000Z-full.bak": true,
		"backups/besedka-20260103T001000Z-incr.bak": true,
	}
	for k := range fake.objects {
		if !want[k] {
			t.Errorf("unexpected surviving key %s", k)
		}
	}
	for k := range want {
		if _, ok := fake.objects[k]; !ok {
			t.Errorf("expected key %s to survive prune", k)
		}
	}
}

func TestFinalBackup(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 7, nil)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched.now = func() time.Time { return base }

	// No backup was ever made: the shutdown backup must be a full one.
	if err := sched.FinalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["backups/besedka-20260101T000000Z-full.bak"]; !ok {
		t.Fatal("first shutdown backup should be full")
	}

	// With a healthy chain and pending changes the shutdown backup is incremental.
	if err := st.SetConfig("k", "v"); err != nil {
		t.Fatal(err)
	}
	sched.now = func() time.Time { return base.Add(10 * time.Minute) }
	if err := sched.FinalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["backups/besedka-20260101T001000Z-incr.bak"]; !ok {
		t.Fatal("shutdown backup should be incremental when a chain exists")
	}

	// A shutdown backup with nothing changed since the last one uploads nothing:
	// the previous artifact already captures the state.
	before := fake.count("backups/")
	sched.now = func() time.Time { return base.Add(20 * time.Minute) }
	if err := sched.FinalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.count("backups/"); got != before {
		t.Errorf("empty shutdown backup should upload nothing: had %d artifacts, now %d", before, got)
	}
}

// TestEmptyIncrementalSkipped: a scheduled incremental with no changes since
// the last backup uploads no artifact, but still flushes pending attachments.
func TestEmptyIncrementalSkipped(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	var flushed int
	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 7, func(context.Context) error {
		flushed++
		return nil
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched.now = func() time.Time { return base }
	if err := sched.DoBackup(context.Background()); err != nil {
		t.Fatal(err)
	}

	// No writes since the full backup: the incremental has nothing to ship.
	sched.now = func() time.Time { return base.Add(10 * time.Minute) }
	if err := sched.DoIncrementalBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.count("backups/"); got != 1 {
		t.Errorf("empty incremental should upload nothing, got %d artifacts", got)
	}
	// The attachment flush still runs (once per backup call), so blobs pending
	// from before the last backup are not stranded.
	if flushed != 2 {
		t.Errorf("expected attachment flush on both backups, got %d", flushed)
	}
}

// TestAttachmentFlushOrdering: a flush failure fails FinalBackup (shutdown is
// the last chance to save blobs) but not scheduled backups, and the database
// artifact is uploaded before the flush runs in both cases.
func TestAttachmentFlushOrdering(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeS3("b")
	client := newClient(t, fake)
	st, _ := newStorage(t, dir, "secret")
	defer func() { _ = st.Close() }()

	var uploadedBeforeFlush int
	flushErr := fmt.Errorf("flush boom")
	sched := NewScheduler(st, client, "backups/", time.Hour, 10*time.Minute, 7, func(context.Context) error {
		uploadedBeforeFlush = fake.count("backups/")
		return flushErr
	})

	// Scheduled backup: flush failure is logged, not returned.
	if err := sched.DoBackup(context.Background()); err != nil {
		t.Fatalf("scheduled backup must tolerate a flush failure, got %v", err)
	}
	if uploadedBeforeFlush != 1 {
		t.Errorf("DB artifact must be uploaded before the attachment flush, saw %d artifacts", uploadedBeforeFlush)
	}

	// Shutdown backup with a pending change: flush failure is fatal.
	if err := st.SetConfig("k", "v"); err != nil {
		t.Fatal(err)
	}
	err := sched.FinalBackup(context.Background())
	if err == nil || !strings.Contains(err.Error(), "flush boom") {
		t.Errorf("FinalBackup must surface a flush failure, got %v", err)
	}
	// The DB artifact itself still made it out before the flush failed.
	if got := fake.count("backups/"); got != 2 {
		t.Errorf("expected 2 artifacts despite flush failure, got %d", got)
	}
}
