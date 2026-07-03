package objectstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	algorithm   = "AWS4-HMAC-SHA256"
	serviceName = "s3"
	// emptyPayloadHash is sha256 of an empty body, used for GET/DELETE/LIST.
	emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// encodePath URI-encodes an object key for use in the canonical request and the
// actual request path. Each byte outside the RFC 3986 unreserved set is
// percent-encoded with uppercase hex, EXCEPT '/', which is left as a path
// separator. Go's url.PathEscape escapes '/', so it cannot be used here.
func encodePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if isUnreserved(c) || c == '/' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexUpper[c>>4])
		b.WriteByte(hexUpper[c&0x0f])
	}
	return b.String()
}

// encodeQueryComponent encodes a query key or value per RFC 3986. Unlike
// url.QueryEscape it encodes spaces as %20 (not '+'), which SigV4 requires.
func encodeQueryComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexUpper[c>>4])
		b.WriteByte(hexUpper[c&0x0f])
	}
	return b.String()
}

const hexUpper = "0123456789ABCDEF"

func isUnreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_' || c == '.' || c == '~':
		return true
	}
	return false
}

// canonicalQuery builds the canonical query string from the already-parsed
// request query values: each key and value is RFC3986-encoded and the pairs are
// sorted by encoded key (then value).
func canonicalQuery(query map[string][]string) string {
	if len(query) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(query))
	for k, vs := range query {
		ek := encodeQueryComponent(k)
		for _, v := range vs {
			pairs = append(pairs, ek+"="+encodeQueryComponent(v))
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// sign computes the SigV4 signature for req and sets the Authorization,
// x-amz-date and x-amz-content-sha256 headers. payloadHash is the hex-encoded
// SHA256 of the request body. The Host header is taken from req.Host/req.URL.
func (c *Client) sign(req *http.Request, payloadHash string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	// Signed headers: host, x-amz-content-sha256, x-amz-date (sorted, lowercase).
	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalURI := encodePath(req.URL.Path)
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery(req.URL.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + c.cfg.Region + "/" + serviceName + "/aws4_request"
	stringToSign := strings.Join([]string{
		algorithm,
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := c.signingKey(dateStamp)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	auth := algorithm +
		" Credential=" + c.cfg.AccessKey + "/" + scope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature
	req.Header.Set("Authorization", auth)
}

func (c *Client) signingKey(dateStamp string) []byte {
	return deriveSigningKey(c.cfg.SecretKey, dateStamp, c.cfg.Region, serviceName)
}

// deriveSigningKey performs the SigV4 chained-HMAC key derivation.
func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
