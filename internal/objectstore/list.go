package objectstore

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"time"
)

type listBucketResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
		ETag         string    `xml:"ETag"`
	} `xml:"Contents"`
}

// List returns all objects whose key starts with prefix, following pagination.
func (c *Client) List(ctx context.Context, prefix string) ([]Object, error) {
	var objects []Object
	var continuation string

	for {
		q := url.Values{}
		q.Set("list-type", "2")
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if continuation != "" {
			q.Set("continuation-token", continuation)
		}

		u, host := c.bucketURL(q.Encode())

		resp, err := c.doRequest(ctx, "GET", u, host, 0, nil)
		if err != nil {
			return nil, err
		}

		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("objectstore: failed to read list response: %w", err)
		}

		var result listBucketResult
		if err := xmlUnmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("objectstore: failed to parse list response: %w", err)
		}

		for _, item := range result.Contents {
			objects = append(objects, Object{
				Key:          item.Key,
				Size:         item.Size,
				LastModified: item.LastModified,
				ETag:         item.ETag,
			})
		}

		if !result.IsTruncated || result.NextContinuationToken == "" {
			break
		}
		continuation = result.NextContinuationToken
	}

	return objects, nil
}

// xmlUnmarshal is a thin wrapper so other files in the package (e.g. error
// parsing) can decode XML without importing encoding/xml directly.
func xmlUnmarshal(data []byte, v any) error {
	return xml.Unmarshal(data, v)
}
