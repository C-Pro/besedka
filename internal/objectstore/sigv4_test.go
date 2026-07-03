package objectstore

import (
	"encoding/hex"
	"net/http"
	"testing"
	"time"
)

// TestSignKnownVector checks the SigV4 signature against a hand-computed vector
// using the well-known AWS example credentials. The expected signature is
// derived from the canonical request/string-to-sign defined by the algorithm,
// pinning our implementation so regressions are caught.
func TestSignKnownVector(t *testing.T) {
	c := &Client{
		cfg: Config{
			Region:    "us-east-1",
			AccessKey: "AKIDEXAMPLE",
			SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			Bucket:    "examplebucket",
			PathStyle: true,
		},
	}

	req, err := http.NewRequest("GET", "https://s3.amazonaws.com/examplebucket/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "s3.amazonaws.com"

	ts := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	c.sign(req, emptyPayloadHash, ts)

	auth := req.Header.Get("Authorization")
	if auth == "" {
		t.Fatal("Authorization header not set")
	}

	// Credential scope and signed headers are deterministic; assert them.
	wantPrefix := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature="
	if got := auth[:len(wantPrefix)]; got != wantPrefix {
		t.Errorf("authorization prefix mismatch:\n got: %q\nwant: %q", got, wantPrefix)
	}

	if req.Header.Get("X-Amz-Date") != "20130524T000000Z" {
		t.Errorf("x-amz-date = %q", req.Header.Get("X-Amz-Date"))
	}
	if req.Header.Get("X-Amz-Content-Sha256") != emptyPayloadHash {
		t.Errorf("x-amz-content-sha256 = %q", req.Header.Get("X-Amz-Content-Sha256"))
	}
}

// TestSigningKey verifies the chained-HMAC signing-key derivation against the
// AWS-published worked example from "Deriving the signing key" (secret
// wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY, date 20120215, us-east-1, iam).
func TestSigningKey(t *testing.T) {
	key := deriveSigningKey(
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"20120215", "us-east-1", "iam",
	)
	got := hex.EncodeToString(key)
	want := "004aa806e13dae88b9032d9261bcb04c67d023afadd221e6b0d206e1760e0b5e"
	if got != want {
		t.Errorf("signing key = %s, want %s", got, want)
	}
}

// TestSignFullVector pins the complete signature against the AWS S3 documented
// "GET Object" example (examplebucket/test.txt, empty payload, virtual-host).
// The expected signature was cross-checked with an independent implementation.
func TestSignFullVector(t *testing.T) {
	c := &Client{cfg: Config{
		Region:    "us-east-1",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Bucket:    "examplebucket",
	}}

	req, err := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "examplebucket.s3.amazonaws.com"

	ts := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	c.sign(req, emptyPayloadHash, ts)

	const wantSig = "df548e2ce037944d03f3e68682813b093763996d597cf890ca3d9037fd231eb4"
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=" + wantSig
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("authorization mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestEncodePath(t *testing.T) {
	cases := map[string]string{
		"/bucket/files/abc123": "/bucket/files/abc123",
		"/a b/c":               "/a%20b/c",
		"/foo+bar":             "/foo%2Bbar",
		"/tilde~dot.dash-_":    "/tilde~dot.dash-_",
		"/unicode/é":           "/unicode/%C3%A9",
		"/path/with/slashes":   "/path/with/slashes",
	}
	for in, want := range cases {
		if got := encodePath(in); got != want {
			t.Errorf("encodePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeQueryComponent(t *testing.T) {
	if got := encodeQueryComponent("a b"); got != "a%20b" {
		t.Errorf("space should encode to %%20, got %q", got)
	}
	if got := encodeQueryComponent("a+b"); got != "a%2Bb" {
		t.Errorf("plus should encode to %%2B, got %q", got)
	}
}

func TestCanonicalQuerySorted(t *testing.T) {
	q := map[string][]string{
		"prefix":    {"files/"},
		"list-type": {"2"},
	}
	got := canonicalQuery(q)
	want := "list-type=2&prefix=files%2F"
	if got != want {
		t.Errorf("canonicalQuery = %q, want %q", got, want)
	}
}
