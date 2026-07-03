package objectstore

import (
	"context"
	"io"
	"net/http"
)

// Get retrieves the object at key. The caller must close the returned
// ReadCloser. If the object does not exist, Get returns ErrNotFound.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	u, host := c.objectURL(key)

	resp, err := c.doRequest(ctx, "GET", u, host, 0, nil)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return resp.Body, nil
}
