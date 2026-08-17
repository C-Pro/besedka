package certmanager

import (
	"bytes"
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"besedka/internal/config"
	"besedka/internal/objectstore"
	"besedka/internal/storage"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const (
	S3CertBackupKey   = "certs/backup.json"
	rateLimitFilename = "ratelimit.json"
)

var (
	reRetryAfterMST   = regexp.MustCompile(`(?i)retry after (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [A-Z]{3})`)
	reRetryAfterRFC   = regexp.MustCompile(`(?i)retry after (\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))`)
)

// CertBackup is the encrypted payload stored in S3 containing the certificate,
// key, associated cache items, and rate limit status.
type CertBackup struct {
	Domain         string            `json:"domain"`
	Expiry         time.Time         `json:"expiry"`
	CacheItems     map[string][]byte `json:"cache_items"`
	RateLimitUntil time.Time         `json:"rate_limit_until,omitempty"`
}

type rateLimitFile struct {
	RateLimitUntil time.Time `json:"rate_limit_until"`
}

type Manager struct {
	cfg      *config.Config
	obj      *objectstore.Client
	autocert *autocert.Manager
	dirCache autocert.DirCache
	domain   string
	now      func() time.Time

	mu             sync.Mutex
	rateLimitUntil time.Time
}

type wrappedCache struct {
	m *Manager
}

func (w *wrappedCache) Get(ctx context.Context, key string) ([]byte, error) {
	return w.m.dirCache.Get(ctx, key)
}

func (w *wrappedCache) Put(ctx context.Context, key string, data []byte) error {
	if err := w.m.dirCache.Put(ctx, key, data); err != nil {
		return err
	}
	if isCertData(data) {
		w.m.onCertUpdated(ctx, key, data)
	}
	return nil
}

func (w *wrappedCache) Delete(ctx context.Context, key string) error {
	return w.m.dirCache.Delete(ctx, key)
}

func New(cfg *config.Config, obj *objectstore.Client) (*Manager, error) {
	if cfg.TLSAutoCertPath == "" {
		return nil, errors.New("certmanager: TLSAutoCertPath is required")
	}

	hostURL, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("certmanager: invalid BaseURL: %w", err)
	}
	domain := hostURL.Hostname()
	if domain == "" {
		return nil, fmt.Errorf("certmanager: missing hostname in BaseURL %q", cfg.BaseURL)
	}

	dirCache := autocert.DirCache(cfg.TLSAutoCertPath)

	m := &Manager{
		cfg:      cfg,
		obj:      obj,
		dirCache: dirCache,
		domain:   domain,
		now:      time.Now,
	}

	autocertMgr := &autocert.Manager{
		Cache:      &wrappedCache{m: m},
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
	}
	m.autocert = autocertMgr

	return m, nil
}

// Init inspects the local cert directory, S3 backup, and rate limit state.
// Order of checking: 1) local cert directory, 2) S3 backup, 3) Let's Encrypt fallback.
func (m *Manager) Init(ctx context.Context) error {
	now := m.now()

	// Load local rate limit state if active
	if until, err := m.loadLocalRateLimit(); err == nil && now.Before(until) {
		m.mu.Lock()
		m.rateLimitUntil = until
		m.mu.Unlock()
		slog.Info("loaded active Let's Encrypt rate limit from local cert dir", "until", until.Format(time.RFC3339))
	}

	// Step 1: Check local cert directory
	if hasValidCachedCert(m.dirCache, m.domain, now) {
		slog.Info("valid certificate found in local cert directory", "domain", m.domain)
		if m.obj != nil && m.cfg.S3Enabled() {
			go m.backupLocalCertToS3(context.Background())
		}
		return nil
	}

	// Step 2: Check S3 backup
	if m.obj != nil && m.cfg.S3Enabled() {
		restored, err := m.restoreFromS3(ctx)
		if err != nil {
			if !errors.Is(err, objectstore.ErrNotFound) {
				slog.Warn("failed to restore certificate backup from S3", "error", err)
			}
		} else if restored {
			slog.Info("restored valid certificate from S3 backup", "domain", m.domain)
			return nil
		}
	}

	slog.Info("no valid certificate found in cert dir or S3; will request from Let's Encrypt when needed", "domain", m.domain)
	return nil
}

func (m *Manager) TLSConfig() *tls.Config {
	cfg := m.autocert.TLSConfig()
	cfg.GetCertificate = m.GetCertificate
	return cfg
}

func (m *Manager) EnsureCert(ctx context.Context) error {
	now := m.now()

	m.mu.Lock()
	until := m.rateLimitUntil
	m.mu.Unlock()

	if !until.IsZero() && now.Before(until) {
		slog.Info("skipping proactive cert request due to active rate limit",
			"domain", m.domain, "rateLimitUntil", until.Format(time.RFC3339))
		return nil
	}

	if hasValidCachedCert(m.dirCache, m.domain, now) {
		return nil
	}

	slog.Info("proactively requesting certificate from Let's Encrypt", "domain", m.domain)
	hello := &tls.ClientHelloInfo{ServerName: m.domain}
	if _, err := m.GetCertificate(hello); err != nil {
		return fmt.Errorf("certificate request for %s failed: %w", m.domain, err)
	}
	slog.Info("successfully obtained certificate from Let's Encrypt", "domain", m.domain)
	return nil
}

func (m *Manager) HTTPHandler(fallback http.Handler) http.Handler {
	return m.autocert.HTTPHandler(fallback)
}

func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	ctx := context.Background()
	now := m.now()

	m.mu.Lock()
	until := m.rateLimitUntil
	m.mu.Unlock()

	if !until.IsZero() && now.Before(until) {
		if hasValidCachedCert(m.dirCache, hello.ServerName, now) {
			return m.autocert.GetCertificate(hello)
		}
		if cert, err := loadCachedCertIgnoringExpiry(m.dirCache, hello.ServerName); err == nil {
			var expiry string
			if cert.Leaf != nil {
				expiry = cert.Leaf.NotAfter.Format(time.RFC3339)
			}
			slog.Warn("serving expired cached certificate during Let's Encrypt rate limit window",
				"serverName", hello.ServerName,
				"certExpiry", expiry,
				"rateLimitUntil", until.Format(time.RFC3339),
			)
			return cert, nil
		}
		return nil, fmt.Errorf("acme rate limit active until %s, retry after that time", until.Format(time.RFC3339))
	}

	cert, err := m.autocert.GetCertificate(hello)
	if err == nil {
		m.clearRateLimit(ctx)
		return cert, nil
	}

	if newUntil, ok := parseRateLimitError(err, now); ok {
		m.setRateLimit(ctx, newUntil, err)
	}

	return nil, err
}

func (m *Manager) backupLocalCertToS3(ctx context.Context) {
	certData, err := m.dirCache.Get(ctx, m.domain)
	if err != nil {
		return
	}
	m.onCertUpdated(ctx, m.domain, certData)
}

func (m *Manager) onCertUpdated(ctx context.Context, key string, certData []byte) {
	expiry, err := extractCertExpiry(certData)
	if err != nil {
		slog.Warn("failed to parse cert expiry for S3 backup", "key", key, "error", err)
		return
	}

	items := map[string][]byte{
		key: certData,
	}
	if acct, err := m.dirCache.Get(ctx, "acme_account+key"); err == nil {
		items["acme_account+key"] = acct
	}

	m.mu.Lock()
	rateLimitUntil := m.rateLimitUntil
	m.mu.Unlock()

	backup := CertBackup{
		Domain:         key,
		Expiry:         expiry,
		CacheItems:     items,
		RateLimitUntil: rateLimitUntil,
	}

	jsonBytes, err := json.Marshal(backup)
	if err != nil {
		slog.Error("failed to marshal cert backup", "error", err)
		return
	}

	if m.cfg.AuthSecret == "" {
		slog.Error("AUTH_SECRET is required to encrypt cert backup")
		return
	}

	crypter, err := storage.NewCrypter([]byte(m.cfg.AuthSecret), nil)
	if err != nil {
		slog.Error("failed to create crypter for cert backup", "error", err)
		return
	}

	ciphertext, err := crypter.Encrypt(jsonBytes)
	if err != nil {
		slog.Error("failed to encrypt cert backup", "error", err)
		return
	}

	payload := append(crypter.Salt(), ciphertext...)

	if m.obj != nil && m.cfg.S3Enabled() {
		if err := m.obj.Put(ctx, S3CertBackupKey, bytes.NewReader(payload), int64(len(payload))); err != nil {
			slog.Error("failed to upload cert backup to S3", "error", err)
		} else {
			slog.Info("successfully backed up encrypted certificate to S3", "domain", key, "expiry", expiry.Format(time.RFC3339))
		}
	}
}

func (m *Manager) restoreFromS3(ctx context.Context) (bool, error) {
	rc, err := m.obj.Get(ctx, S3CertBackupKey)
	if err != nil {
		return false, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return false, fmt.Errorf("read S3 cert backup: %w", err)
	}

	if len(data) < storage.SaltLen {
		return false, errors.New("S3 cert backup payload too short")
	}

	if m.cfg.AuthSecret == "" {
		return false, errors.New("AUTH_SECRET is required")
	}

	salt := data[:storage.SaltLen]
	ciphertext := data[storage.SaltLen:]

	crypter, err := storage.NewCrypter([]byte(m.cfg.AuthSecret), salt)
	if err != nil {
		return false, fmt.Errorf("create crypter: %w", err)
	}

	jsonBytes, err := crypter.Decrypt(ciphertext)
	if err != nil {
		return false, fmt.Errorf("decrypt S3 cert backup: %w", err)
	}

	var backup CertBackup
	if err := json.Unmarshal(jsonBytes, &backup); err != nil {
		return false, fmt.Errorf("unmarshal S3 cert backup: %w", err)
	}

	now := m.now()
	if now.After(backup.Expiry) {
		slog.Warn("S3 cert backup is expired", "domain", backup.Domain, "expiry", backup.Expiry.Format(time.RFC3339))
		return false, nil
	}

	for k, v := range backup.CacheItems {
		if err := m.dirCache.Put(ctx, k, v); err != nil {
			slog.Error("failed to write restored cert item to dir cache", "key", k, "error", err)
		}
	}

	if !backup.RateLimitUntil.IsZero() && now.Before(backup.RateLimitUntil) {
		m.mu.Lock()
		m.rateLimitUntil = backup.RateLimitUntil
		m.mu.Unlock()
		_ = m.saveLocalRateLimit(backup.RateLimitUntil)
		slog.Info("restored active rate limit state from S3 backup", "until", backup.RateLimitUntil.Format(time.RFC3339))
	}

	return true, nil
}

func (m *Manager) setRateLimit(ctx context.Context, until time.Time, origErr error) {
	m.mu.Lock()
	m.rateLimitUntil = until
	m.mu.Unlock()

	slog.Error("Let's Encrypt rate limit hit; will not attempt re-issuance before retry-after",
		"retry_after", until.Format(time.RFC3339),
		"error", origErr,
	)

	_ = m.saveLocalRateLimit(until)

	if m.obj != nil && m.cfg.S3Enabled() {
		go func() {
			if certData, err := m.dirCache.Get(context.Background(), m.domain); err == nil {
				m.onCertUpdated(context.Background(), m.domain, certData)
			}
		}()
	}
}

func (m *Manager) clearRateLimit(ctx context.Context) {
	m.mu.Lock()
	if m.rateLimitUntil.IsZero() {
		m.mu.Unlock()
		return
	}
	m.rateLimitUntil = time.Time{}
	m.mu.Unlock()

	_ = m.removeLocalRateLimit()
}

func (m *Manager) saveLocalRateLimit(until time.Time) error {
	if err := os.MkdirAll(m.cfg.TLSAutoCertPath, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(rateLimitFile{RateLimitUntil: until})
	if err != nil {
		return err
	}
	filePath := filepath.Join(m.cfg.TLSAutoCertPath, rateLimitFilename)
	return os.WriteFile(filePath, data, 0600)
}

func (m *Manager) loadLocalRateLimit() (time.Time, error) {
	filePath := filepath.Join(m.cfg.TLSAutoCertPath, rateLimitFilename)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return time.Time{}, err
	}
	var rf rateLimitFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return time.Time{}, err
	}
	return rf.RateLimitUntil, nil
}

func (m *Manager) removeLocalRateLimit() error {
	filePath := filepath.Join(m.cfg.TLSAutoCertPath, rateLimitFilename)
	err := os.Remove(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func isCertData(data []byte) bool {
	_, err := extractCertExpiry(data)
	return err == nil
}

func extractCertExpiry(data []byte) (time.Time, error) {
	var leaf *x509.Certificate
	rest := data
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err == nil {
				if leaf == nil || (!cert.IsCA && leaf.IsCA) {
					leaf = cert
				}
			}
		}
	}
	if leaf == nil {
		return time.Time{}, errors.New("no certificate found in data")
	}
	return leaf.NotAfter, nil
}

func hasValidCachedCert(dirCache autocert.DirCache, domain string, now time.Time) bool {
	if domain == "" {
		return false
	}
	domain = strings.TrimSuffix(domain, ".")
	data, err := dirCache.Get(context.Background(), domain)
	if err != nil {
		return false
	}
	expiry, err := extractCertExpiry(data)
	if err != nil {
		return false
	}
	return now.Before(expiry)
}

func loadCachedCertIgnoringExpiry(dirCache autocert.DirCache, domain string) (*tls.Certificate, error) {
	if domain == "" {
		return nil, errors.New("empty domain")
	}
	domain = strings.TrimSuffix(domain, ".")
	data, err := dirCache.Get(context.Background(), domain)
	if err != nil {
		return nil, err
	}

	var (
		privKey crypto.Signer
		pubDER  [][]byte
	)
	rest := data
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if strings.Contains(block.Type, "PRIVATE KEY") {
			key, err := parsePrivateKey(block.Bytes)
			if err == nil {
				privKey = key
			}
		} else if block.Type == "CERTIFICATE" {
			pubDER = append(pubDER, block.Bytes)
		}
	}

	if privKey == nil || len(pubDER) == 0 {
		return nil, errors.New("invalid cert cache format")
	}

	leaf, err := x509.ParseCertificate(pubDER[0])
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: pubDER,
		PrivateKey:  privKey,
		Leaf:        leaf,
	}, nil
}

func parsePrivateKey(der []byte) (crypto.Signer, error) {
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("unknown private key type")
}

func parseRateLimitError(err error, now time.Time) (time.Time, bool) {
	if err == nil {
		return time.Time{}, false
	}

	var acmeErr *acme.Error
	if errors.As(err, &acmeErr) {
		if dur, ok := acme.RateLimit(acmeErr); ok {
			if dur > 0 {
				return now.Add(dur), true
			}
		}
	} else if dur, ok := acme.RateLimit(err); ok {
		if dur > 0 {
			return now.Add(dur), true
		}
	}

	errMsg := err.Error()
	errMsgLower := strings.ToLower(errMsg)

	isRateLimit := strings.Contains(errMsgLower, "ratelimited") ||
		strings.Contains(errMsgLower, "rate limit") ||
		strings.Contains(errMsgLower, "too many certificates") ||
		strings.Contains(errMsgLower, "429")

	if !isRateLimit {
		return time.Time{}, false
	}

	if matches := reRetryAfterMST.FindStringSubmatch(errMsg); len(matches) > 1 {
		if t, parseErr := time.Parse("2006-01-02 15:04:05 MST", matches[1]); parseErr == nil {
			return t, true
		}
	}

	if matches := reRetryAfterRFC.FindStringSubmatch(errMsg); len(matches) > 1 {
		if t, parseErr := time.Parse(time.RFC3339, matches[1]); parseErr == nil {
			return t, true
		}
	}

	return now.Add(1 * time.Hour), true
}
