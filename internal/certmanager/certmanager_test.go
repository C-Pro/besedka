package certmanager

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"besedka/internal/config"
	"besedka/internal/objectstore"
	"besedka/internal/storage"

	"golang.org/x/crypto/acme"
)

func generateTestCert(t *testing.T, domain string, notBefore, notAfter time.Time) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ecdsa key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames:              []string{domain},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	var buf bytes.Buffer
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	if err := pem.Encode(&buf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("failed to encode key pem: %v", err)
	}
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("failed to encode cert pem: %v", err)
	}

	return buf.Bytes(), priv
}

func TestParseRateLimitError(t *testing.T) {
	now := time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		err         error
		wantLimit   bool
		wantUntil   time.Time
		allowMargin bool
	}{
		{
			name:      "nil error",
			err:       nil,
			wantLimit: false,
		},
		{
			name:      "generic error",
			err:       errors.New("connection refused"),
			wantLimit: false,
		},
		{
			name:      "user error message from issue prompt",
			err:       errors.New("2026/08/14 14:37:19 http: TLS handshake error from 104.23.217.30:10281: 429 urn:ietf:params:acme:error:rateLimited: too many certificates (5) already issued for this exact set of identifiers in the last 168h0m0s, retry after 2026-08-15 19:14:33 UTC: see https://letsencrypt.org/docs/rate-limits/#new-certificates-per-exact-set-of-identifiers"),
			wantLimit: true,
			wantUntil: time.Date(2026, 8, 15, 19, 14, 33, 0, time.UTC),
		},
		{
			name:      "RFC3339 timestamp in rate limited error",
			err:       errors.New("429 rateLimited: retry after 2026-08-15T20:00:00Z"),
			wantLimit: true,
			wantUntil: time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC),
		},
		{
			name:        "generic rate limit fallback duration",
			err:         errors.New("429 rateLimited: too many requests"),
			wantLimit:   true,
			wantUntil:   now.Add(1 * time.Hour),
			allowMargin: true,
		},
		{
			name: "acme.Error with Retry-After header",
			err: &acme.Error{
				ProblemType: "urn:ietf:params:acme:error:rateLimited",
				Header: http.Header{
					"Retry-After": []string{"3600"},
				},
			},
			wantLimit:   true,
			wantUntil:   now.Add(1 * time.Hour),
			allowMargin: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUntil, gotLimit := parseRateLimitError(tt.err, now)
			if gotLimit != tt.wantLimit {
				t.Fatalf("expected limit %v, got %v", tt.wantLimit, gotLimit)
			}
			if tt.wantLimit {
				if tt.allowMargin {
					diff := gotUntil.Sub(tt.wantUntil)
					if diff < -5*time.Second || diff > 5*time.Second {
						t.Errorf("expected until around %v, got %v", tt.wantUntil, gotUntil)
					}
				} else if !gotUntil.Equal(tt.wantUntil) {
					t.Errorf("expected until %v, got %v", tt.wantUntil, gotUntil)
				}
			}
		})
	}
}

func TestCertManager_Init_LocalFirst(t *testing.T) {
	tempDir := t.TempDir()
	domain := "example.com"
	certBytes, _ := generateTestCert(t, domain, time.Now().Add(-1*time.Hour), time.Now().Add(24*time.Hour))

	// Write valid cert to local directory
	err := os.WriteFile(filepath.Join(tempDir, domain), certBytes, 0600)
	if err != nil {
		t.Fatalf("failed to write local cert: %v", err)
	}

	cfg := &config.Config{
		TLSAutoCertPath: tempDir,
		BaseURL:         "https://" + domain,
		AuthSecret:      "test-auth-secret",
	}

	cm, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create certmanager: %v", err)
	}

	if err := cm.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify local cert exists and is recognized as valid
	if !hasValidCachedCert(cm.dirCache, domain, time.Now()) {
		t.Errorf("expected valid cached cert in local dir")
	}
}

func TestCertManager_S3RestoreAndBackup(t *testing.T) {
	// Setup fake S3 server
	s3Data := make(map[string][]byte)
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
		switch r.Method {
		case "PUT":
			buf, _ := io.ReadAll(r.Body)
			s3Data[key] = buf
			w.WriteHeader(http.StatusOK)
		case "GET":
			val, ok := s3Data[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(val)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer s3Server.Close()

	domain := "chat.example.com"
	authSecret := "very-secret-encryption-key-12345"

	objClient, err := objectstore.New(objectstore.Config{
		Endpoint:  s3Server.URL,
		Region:    "us-east-1",
		Bucket:    "test-bucket",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("failed to create objectstore client: %v", err)
	}

	certBytes, _ := generateTestCert(t, domain, time.Now().Add(-1*time.Hour), time.Now().Add(48*time.Hour))
	expiry, err := extractCertExpiry(certBytes)
	if err != nil {
		t.Fatalf("failed to extract cert expiry: %v", err)
	}

	// Manually construct encrypted S3 backup
	backupStruct := CertBackup{
		Domain: domain,
		Expiry: expiry,
		CacheItems: map[string][]byte{
			domain:             certBytes,
			"acme_account+key": []byte("fake-account-key-pem"),
		},
	}
	jsonBytes, err := json.Marshal(backupStruct)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	crypter, err := storage.NewCrypter([]byte(authSecret), nil)
	if err != nil {
		t.Fatalf("crypter creation failed: %v", err)
	}
	ciphertext, err := crypter.Encrypt(jsonBytes)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	payload := append(crypter.Salt(), ciphertext...)
	s3Data[S3CertBackupKey] = payload

	// Init certmanager with EMPTY local directory
	emptyDir := t.TempDir()
	cfg := &config.Config{
		TLSAutoCertPath: emptyDir,
		BaseURL:         "https://" + domain,
		AuthSecret:      authSecret,
		S3Endpoint:      s3Server.URL,
		S3Bucket:        "test-bucket",
		S3AccessKey:     "minioadmin",
		S3SecretKey:     "minioadmin",
	}

	cm, err := New(cfg, objClient)
	if err != nil {
		t.Fatalf("New certmanager failed: %v", err)
	}

	if err := cm.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify cert was restored into empty local directory
	restoredCertData, err := os.ReadFile(filepath.Join(emptyDir, domain))
	if err != nil {
		t.Fatalf("restored cert file missing from local dir: %v", err)
	}
	if !bytes.Equal(restoredCertData, certBytes) {
		t.Errorf("restored cert data does not match original cert")
	}

	restoredAcctKey, err := os.ReadFile(filepath.Join(emptyDir, "acme_account+key"))
	if err != nil {
		t.Fatalf("restored account key missing: %v", err)
	}
	if string(restoredAcctKey) != "fake-account-key-pem" {
		t.Errorf("restored account key mismatch")
	}
}

func TestCertManager_RateLimitEnforcement(t *testing.T) {
	tempDir := t.TempDir()
	domain := "ratelimit.example.com"
	authSecret := "secret-key-123"

	cfg := &config.Config{
		TLSAutoCertPath: tempDir,
		BaseURL:         "https://" + domain,
		AuthSecret:      authSecret,
	}

	cm, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Mock rate limit until 1 hour from now
	rateLimitUntil := time.Now().Add(1 * time.Hour)
	cm.setRateLimit(context.Background(), rateLimitUntil, errors.New("429 rateLimited"))

	// Verify local ratelimit.json file was written
	loadedUntil, err := cm.loadLocalRateLimit()
	if err != nil {
		t.Fatalf("failed to load local rate limit file: %v", err)
	}
	if !loadedUntil.Equal(rateLimitUntil) {
		t.Errorf("expected loaded rate limit %v, got %v", rateLimitUntil, loadedUntil)
	}

	// Attempt GetCertificate while rate limit is active and NO cert is cached
	hello := &tls.ClientHelloInfo{
		ServerName: domain,
	}
	_, err = cm.GetCertificate(hello)
	if err == nil {
		t.Fatalf("expected error due to active rate limit, got nil")
	}
	if !strings.Contains(err.Error(), "acme rate limit active until") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCertManager_S3ExpiredBackupRejected(t *testing.T) {
	s3Data := make(map[string][]byte)
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
		switch r.Method {
		case "GET":
			val, ok := s3Data[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(val)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer s3Server.Close()

	domain := "expired.example.com"
	authSecret := "secret-for-expired-test"

	objClient, err := objectstore.New(objectstore.Config{
		Endpoint:  s3Server.URL,
		Region:    "us-east-1",
		Bucket:    "test-bucket",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("failed to create objectstore client: %v", err)
	}

	certBytes, _ := generateTestCert(t, domain, time.Now().Add(-48*time.Hour), time.Now().Add(-1*time.Hour))
	expiry, err := extractCertExpiry(certBytes)
	if err != nil {
		t.Fatalf("failed to extract cert expiry: %v", err)
	}

	backupStruct := CertBackup{
		Domain: domain,
		Expiry: expiry,
		CacheItems: map[string][]byte{
			domain: certBytes,
		},
	}
	jsonBytes, err := json.Marshal(backupStruct)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	crypter, err := storage.NewCrypter([]byte(authSecret), nil)
	if err != nil {
		t.Fatalf("crypter creation failed: %v", err)
	}
	ciphertext, err := crypter.Encrypt(jsonBytes)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	payload := append(crypter.Salt(), ciphertext...)
	s3Data[S3CertBackupKey] = payload

	emptyDir := t.TempDir()
	cfg := &config.Config{
		TLSAutoCertPath: emptyDir,
		BaseURL:         "https://" + domain,
		AuthSecret:      authSecret,
		S3Endpoint:      s3Server.URL,
		S3Bucket:        "test-bucket",
		S3AccessKey:     "minioadmin",
		S3SecretKey:     "minioadmin",
	}

	cm, err := New(cfg, objClient)
	if err != nil {
		t.Fatalf("New certmanager failed: %v", err)
	}

	if err := cm.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	entries, err := os.ReadDir(emptyDir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir after rejecting expired backup, got %d entries", len(entries))
	}
}
