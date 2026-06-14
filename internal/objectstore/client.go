// Package objectstore is a minimal, dependency-free client for S3-compatible
// object storage. It speaks the S3 REST API with AWS Signature Version 4 auth,
// implemented with the Go standard library only. It supports the small subset
// Besedka needs: Put, Get, List (ListObjectsV2) and Delete.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound is returned by Get when the object does not exist (HTTP 404).
var ErrNotFound = errors.New("objectstore: not found")

// maxSinglePut is the S3 limit for a single PUT (5 GiB). Larger objects require
// multipart upload, which is intentionally not implemented.
const maxSinglePut = 5 << 30

// Config holds connection settings for an S3-compatible endpoint.
type Config struct {
	Endpoint   string // e.g. "https://s3.us-east-1.amazonaws.com" or "http://localhost:9000"
	Region     string // default "us-east-1"
	Bucket     string
	AccessKey  string
	SecretKey  string
	PathStyle  bool         // default true (MinIO/self-hosted); false => virtual-host
	HTTPClient *http.Client // optional; default 30s timeout
}

// Client is a configured S3-compatible object storage client.
type Client struct {
	cfg        Config
	endpoint   *url.URL
	httpClient *http.Client
	// now returns the current time; overridable in tests.
	now func() time.Time
}

// Object describes a single object returned by List.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

// New builds a Client. It returns (nil, nil) when the feature is disabled, i.e.
// when either Bucket or Endpoint is empty, so callers can treat a nil client as
// "object storage off".
func New(cfg Config) (*Client, error) {
	if cfg.Bucket == "" || cfg.Endpoint == "" {
		return nil, nil
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("objectstore: access key and secret key are required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	ep, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("objectstore: invalid endpoint: %w", err)
	}
	if ep.Scheme == "" || ep.Host == "" {
		return nil, fmt.Errorf("objectstore: endpoint must include scheme and host: %q", cfg.Endpoint)
	}

	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{
		cfg:        cfg,
		endpoint:   ep,
		httpClient: hc,
		now:        time.Now,
	}, nil
}

// objectURL builds the request URL and Host header for the given object key,
// honoring path-style vs virtual-host addressing.
func (c *Client) objectURL(key string) (u *url.URL, host string) {
	host = c.endpoint.Host
	out := &url.URL{Scheme: c.endpoint.Scheme}
	if c.cfg.PathStyle {
		out.Host = host
		out.Path = "/" + c.cfg.Bucket + "/" + key
	} else {
		host = c.cfg.Bucket + "." + c.endpoint.Host
		out.Host = host
		out.Path = "/" + key
	}
	// Ensure Go emits exactly our SigV4 path encoding.
	out.RawPath = encodePath(out.Path)
	return out, host
}

// bucketURL builds the request URL for a bucket-level operation (e.g. List),
// applying the given raw query string.
func (c *Client) bucketURL(rawQuery string) (u *url.URL, host string) {
	host = c.endpoint.Host
	out := &url.URL{Scheme: c.endpoint.Scheme, RawQuery: rawQuery}
	if c.cfg.PathStyle {
		out.Host = host
		out.Path = "/" + c.cfg.Bucket
	} else {
		host = c.cfg.Bucket + "." + c.endpoint.Host
		out.Host = host
		out.Path = "/"
	}
	return out, host
}

// bodyFunc produces a fresh body reader and its payload hash for each attempt,
// so requests can be safely re-signed and retried.
type bodyFunc func() (body io.ReadCloser, payloadHash string, err error)

// doRequest signs and sends the request, retrying transient failures. newBody
// is called once per attempt to obtain a fresh body + payload hash. It may be
// nil for bodyless requests (an empty payload hash is used). contentLength is
// the body size in bytes (0 for bodyless requests); it is set explicitly so Go
// never falls back to chunked transfer encoding.
func (c *Client) doRequest(ctx context.Context, method string, u *url.URL, host string, contentLength int64, newBody bodyFunc) (*http.Response, error) {
	const maxAttempts = 3
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := backoff(ctx, attempt); err != nil {
				return nil, err
			}
		}

		var body io.ReadCloser
		payloadHash := emptyPayloadHash
		if newBody != nil {
			b, ph, err := newBody()
			if err != nil {
				return nil, err
			}
			body, payloadHash = b, ph
		}

		req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
		if err != nil {
			return nil, err
		}
		req.Host = host
		req.ContentLength = contentLength
		req.URL.RawPath = encodePath(req.URL.Path)

		c.sign(req, payloadHash, c.now())

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue // network error: retry
		}

		if resp.StatusCode < 300 {
			return resp, nil
		}

		// Error response: decide whether to retry.
		apiErr := parseAPIError(resp)
		if !isRetryable(resp.StatusCode, apiErr) || attempt == maxAttempts-1 {
			return nil, apiErr
		}
		lastErr = apiErr
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("objectstore: request failed after retries")
	}
	return nil, lastErr
}

// APIError is a non-2xx response from the object store.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("objectstore: status %d: %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("objectstore: status %d", e.StatusCode)
}

// parseAPIError reads (and closes) the error body and returns a typed error.
func parseAPIError(resp *http.Response) *APIError {
	defer func() { _ = resp.Body.Close() }()
	e := &APIError{StatusCode: resp.StatusCode}
	// Error bodies are small; cap the read defensively.
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if len(data) > 0 {
		var x struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		}
		if err := xmlUnmarshal(data, &x); err == nil {
			e.Code = x.Code
			e.Message = x.Message
		}
	}
	return e
}

func isRetryable(status int, e *APIError) bool {
	if status == http.StatusTooManyRequests || status >= 500 {
		return true
	}
	// Clock skew: re-signing with a fresh timestamp on the next attempt may fix it.
	if e != nil && strings.Contains(e.Code, "RequestTimeTooSkewed") {
		return true
	}
	return false
}

// backoff sleeps with exponential backoff + jitter, respecting ctx.
func backoff(ctx context.Context, attempt int) error {
	d := time.Duration(200<<uint(attempt-1)) * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
