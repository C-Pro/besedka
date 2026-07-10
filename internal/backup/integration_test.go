//go:build integration

package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"besedka/internal/filestore"
	"besedka/internal/objectstore"
	"besedka/internal/storage"
)

// newIntegrationClient builds a client from the S3_* environment, skipping the
// test when object storage is not configured.
func newIntegrationClient(t *testing.T) *objectstore.Client {
	t.Helper()
	endpoint := os.Getenv("S3_ENDPOINT")
	bucket := os.Getenv("S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("set S3_ENDPOINT and S3_BUCKET to run the integration test")
	}
	c, err := objectstore.New(objectstore.Config{
		Endpoint:  endpoint,
		Region:    getenvOr("S3_REGION", "us-east-1"),
		Bucket:    bucket,
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func getenvOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func openStorage(t *testing.T, dir, secret string) (*storage.BboltStorage, string) {
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

// uniquePrefix isolates each run so leftover objects don't affect retention.
func uniquePrefix() string {
	return "it-backups-" + time.Now().UTC().Format("20060102T150405.000Z") + "/"
}

// TestIntegrationBackupAndRecover runs the full encrypt -> upload -> delete DB
// -> download -> decrypt -> reopen cycle against a real S3-compatible server.
func TestIntegrationBackupAndRecover(t *testing.T) {
	client := newIntegrationClient(t)
	prefix := uniquePrefix()
	ctx := context.Background()

	const secret = "integration-secret"
	dir := t.TempDir()
	st, dbPath := openStorage(t, dir, secret)
	if err := st.SetConfig("motto", "besedka-rules"); err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(st, client, prefix, time.Hour, 10*time.Minute, 7, nil)
	if err := sched.BackupOnce(ctx); err != nil {
		t.Fatalf("BackupOnce: %v", err)
	}

	// Extend the chain with an incremental before recovering.
	if err := st.SetConfig("motto2", "incrementals-work"); err != nil {
		t.Fatal(err)
	}
	if err := sched.IncrementalOnce(ctx); err != nil {
		t.Fatalf("IncrementalOnce: %v", err)
	}
	_ = st.Close()

	// Verify both artifacts actually exist in the bucket.
	objs, err := client.List(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("expected 2 backup objects (full + incremental), got %d", len(objs))
	}
	t.Cleanup(func() {
		for _, o := range objs {
			_ = client.Delete(ctx, o.Key)
		}
	})

	// Simulate disk loss.
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}

	recovered, err := RecoverDBIfMissing(ctx, dbPath, secret, prefix, client)
	if err != nil {
		t.Fatalf("RecoverDBIfMissing: %v", err)
	}
	if !recovered {
		t.Fatal("expected recovery to occur")
	}

	st2, _ := openStorage(t, dir, secret)
	defer st2.Close()
	got, err := st2.GetConfig("motto")
	if err != nil {
		t.Fatal(err)
	}
	if got != "besedka-rules" {
		t.Errorf("recovered value = %q, want besedka-rules", got)
	}
	if got, _ := st2.GetConfig("motto2"); got != "incrementals-work" {
		t.Errorf("value from incremental = %q, want incrementals-work", got)
	}
}

// TestIntegrationWrongSecret verifies decryption fails against a real backup
// when the AUTH_SECRET differs, and that no DB file is left behind.
func TestIntegrationWrongSecret(t *testing.T) {
	client := newIntegrationClient(t)
	prefix := uniquePrefix()
	ctx := context.Background()

	dir := t.TempDir()
	st, dbPath := openStorage(t, dir, "correct-secret")
	sched := NewScheduler(st, client, prefix, time.Hour, 0, 7, nil)
	if err := sched.BackupOnce(ctx); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	objs, _ := client.List(ctx, prefix)
	t.Cleanup(func() {
		for _, o := range objs {
			_ = client.Delete(ctx, o.Key)
		}
	})

	_ = os.Remove(dbPath)

	if _, err := RecoverDBIfMissing(ctx, dbPath, "wrong-secret", prefix, client); err == nil {
		t.Fatal("expected decryption to fail with wrong secret")
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Error("no DB file should exist after failed decryption")
	}
}

// TestIntegrationRetention verifies old backups are pruned on a real server.
func TestIntegrationRetention(t *testing.T) {
	client := newIntegrationClient(t)
	prefix := uniquePrefix()
	ctx := context.Background()

	dir := t.TempDir()
	st, _ := openStorage(t, dir, "secret")
	defer st.Close()

	sched := NewScheduler(st, client, prefix, time.Hour, 0, 2, nil)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Hour)
		sched.now = func() time.Time { return ts }
		if err := sched.BackupOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}

	objs, err := client.List(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, o := range objs {
			_ = client.Delete(ctx, o.Key)
		}
	})
	if len(objs) != 2 {
		t.Fatalf("expected 2 backups after retention, got %d", len(objs))
	}
}
