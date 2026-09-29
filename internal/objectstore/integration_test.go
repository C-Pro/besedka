//go:build integration

package objectstore

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// TestMinIORoundTrip exercises the client against a real S3-compatible server
// (e.g. S3Mock or MinIO), which is the ultimate check that our SigV4 signing is accepted.
//
// Run a local S3Mock container first, e.g.:
//
//	docker run -d --rm -p 9090:9090 -e initialBuckets=besedka-test -e COM_ADOBE_TESTING_S3MOCK_STORE_INITIAL_BUCKETS=besedka-test adobe/s3mock:latest
//
// Then:
//
//	S3_ENDPOINT=http://localhost:9090 S3_BUCKET=besedka-test \
//	S3_ACCESS_KEY=test S3_SECRET_KEY=test \
//	  go test -tags integration ./internal/objectstore/
func TestMinIORoundTrip(t *testing.T) {
	endpoint := os.Getenv("S3_ENDPOINT")
	bucket := os.Getenv("S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("set S3_ENDPOINT and S3_BUCKET to run the integration test")
	}

	c, err := New(Config{
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

	ctx := context.Background()
	key := "objectstore-integration/test.bin"
	payload := []byte("integration payload \x00\x01\x02 ~+/=")

	if err := c.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Errorf("Get returned %q, want %q", got, payload)
	}

	objs, err := c.List(ctx, "objectstore-integration/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, o := range objs {
		if o.Key == key {
			found = true
			if o.Size != int64(len(payload)) {
				t.Errorf("listed size = %d, want %d", o.Size, len(payload))
			}
		}
	}
	if !found {
		t.Errorf("List did not return %q", key)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Get(ctx, key); err != ErrNotFound {
		t.Errorf("after delete, Get = %v, want ErrNotFound", err)
	}
}

func getenvOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
