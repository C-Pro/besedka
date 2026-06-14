package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// Put uploads an object under key. size must be the exact length of the data
// readable from r. If r is an io.ReadSeeker, it is hashed and rewound without
// buffering; otherwise the body is read fully into memory (bounded by size).
//
// Objects larger than 5 GiB are rejected: multipart upload is not implemented.
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if size > maxSinglePut {
		return fmt.Errorf("objectstore: object %q is %d bytes; exceeds 5GiB single-PUT limit (multipart not implemented)", key, size)
	}

	u, host := c.objectURL(key)

	newBody, err := seekableBody(r, size)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, "PUT", u, host, size, newBody)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// seekableBody returns a bodyFunc that yields a fresh body + payload hash on
// each call. For an io.ReadSeeker it hashes in one pass and rewinds; otherwise
// it buffers the content once and replays it.
func seekableBody(r io.Reader, size int64) (bodyFunc, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		hash, err := hashSeeker(rs)
		if err != nil {
			return nil, err
		}
		return func() (io.ReadCloser, string, error) {
			if _, err := rs.Seek(0, io.SeekStart); err != nil {
				return nil, "", err
			}
			return io.NopCloser(io.LimitReader(rs, size)), hash, nil
		}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("objectstore: failed to read body: %w", err)
	}
	hash := sha256Hex(data)
	return func() (io.ReadCloser, string, error) {
		return io.NopCloser(bytes.NewReader(data)), hash, nil
	}, nil
}

func hashSeeker(rs io.ReadSeeker) (string, error) {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, rs); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
