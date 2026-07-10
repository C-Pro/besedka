package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"time"
)

var chatNameRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{3,32}$`)

type Config struct {
	DBFile              string
	AdminAddr           string
	APIAddr             string
	BaseURL             string
	UploadsPath         string
	AdminUser           string
	AdminPassword       string
	AuthSecret          string
	TokenExpiry         time.Duration
	MaxImageSize        int64
	MaxAvatarSize       int64
	MaxFileSize         int64
	TLSCert             string
	TLSKey              string
	TLSAutoCertPath     string
	EnableHTTPChallenge bool
	HTTPChallengePort   string
	ChatName            string

	// S3-compatible object storage (optional). Empty bucket or endpoint
	// disables the feature entirely.
	S3Endpoint       string
	S3Region         string
	S3Bucket         string
	S3AccessKey      string
	S3SecretKey      string
	S3PathStyle          bool
	S3BackupInterval     time.Duration
	S3BackupIncrInterval time.Duration
	S3BackupKeep         int64
}

// S3Enabled reports whether object-storage backup/mirroring is configured.
func (c *Config) S3Enabled() bool {
	return c.S3Bucket != "" && c.S3Endpoint != ""
}

func Load() (*Config, error) {
	tokenExpiry, err := time.ParseDuration(getEnv("TOKEN_EXPIRY", "24h"))
	if err != nil {
		return nil, err
	}

	backupInterval, err := time.ParseDuration(getEnv("S3_BACKUP_INTERVAL", "24h"))
	if err != nil {
		return nil, fmt.Errorf("invalid S3_BACKUP_INTERVAL: %w", err)
	}

	// Incremental backups upload only the records changed since the previous
	// backup, so they can run far more often than full snapshots. 0 disables
	// them (full backups only).
	backupIncrInterval, err := time.ParseDuration(getEnv("S3_BACKUP_INCREMENTAL_INTERVAL", "15m"))
	if err != nil {
		return nil, fmt.Errorf("invalid S3_BACKUP_INCREMENTAL_INTERVAL: %w", err)
	}

	apiAddr := os.Getenv("API_ADDR")
	tlsAutoCertPath := os.Getenv("TLS_AUTO_CERT_PATH")
	tlsCert := os.Getenv("TLS_CERT")
	tlsKey := os.Getenv("TLS_KEY")

	if apiAddr == "" {
		if tlsAutoCertPath != "" || (tlsCert != "" && tlsKey != "") {
			apiAddr = ":443"
		} else {
			apiAddr = ":8080"
		}
	}

	cfg := &Config{
		DBFile:              getEnv("BESEDKA_DB", "besedka.db"),
		AdminAddr:           getEnv("ADMIN_ADDR", "localhost:8081"),
		APIAddr:             apiAddr,
		BaseURL:             getEnv("BASE_URL", "http://localhost:8080"),
		UploadsPath:         getEnv("UPLOADS_PATH", "uploads"),
		AdminUser:           getEnv("ADMIN_USER", "admin"),
		AdminPassword:       getEnv("ADMIN_PASSWORD", "1337chat"),
		AuthSecret:          os.Getenv("AUTH_SECRET"),
		TokenExpiry:         tokenExpiry,
		MaxImageSize:        getEnvInt64("MAX_IMAGE_SIZE", 10<<20),
		MaxAvatarSize:       getEnvInt64("MAX_AVATAR_SIZE", 5<<20),
		MaxFileSize:         getEnvInt64("MAX_FILE_SIZE", 25<<20),
		TLSCert:             tlsCert,
		TLSKey:              tlsKey,
		TLSAutoCertPath:     tlsAutoCertPath,
		EnableHTTPChallenge: getEnv("ENABLE_HTTP_CHALLENGE", "false") == "true" || getEnv("ENABLE_HTTP_CHALLENGE", "false") == "1",
		HTTPChallengePort:   getEnv("HTTP_CHALLENGE_PORT", "80"),
		ChatName:            getEnv("CHAT_NAME", "Besedka"),

		S3Endpoint:       os.Getenv("S3_ENDPOINT"),
		S3Region:         getEnv("S3_REGION", "us-east-1"),
		S3Bucket:         os.Getenv("S3_BUCKET"),
		S3AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:      os.Getenv("S3_SECRET_KEY"),
		S3PathStyle:          getEnv("S3_PATH_STYLE", "true") == "true" || getEnv("S3_PATH_STYLE", "true") == "1",
		S3BackupInterval:     backupInterval,
		S3BackupIncrInterval: backupIncrInterval,
		S3BackupKeep:         getEnvInt64("S3_BACKUP_KEEP", 7),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	// The database is always encrypted, so the secret is required even for
	// CLI invocations like --add-user.
	if c.AuthSecret == "" {
		return fmt.Errorf("AUTH_SECRET is required")
	}

	if c.TokenExpiry <= 0 {
		return fmt.Errorf("TOKEN_EXPIRY must be greater than 0")
	}

	if (c.TLSCert != "" && c.TLSKey == "") || (c.TLSCert == "" && c.TLSKey != "") {
		return fmt.Errorf("TLS_CERT and TLS_KEY must be provided together")
	}

	if c.EnableHTTPChallenge && c.TLSAutoCertPath == "" {
		return fmt.Errorf("ENABLE_HTTP_CHALLENGE requires TLS_AUTO_CERT_PATH")
	}

	if _, err := url.Parse(c.BaseURL); err != nil {
		return fmt.Errorf("BASE_URL must be a valid URL: %w", err)
	}

	if !chatNameRegex.MatchString(c.ChatName) {
		return fmt.Errorf("CHAT_NAME must be 3-32 alphanumeric characters or dashes")
	}

	// Object storage: bucket and endpoint must be set together. When enabled,
	// credentials are required. Backups and mirrored files are encrypted with
	// the (always-required) AUTH_SECRET.
	if (c.S3Bucket == "") != (c.S3Endpoint == "") {
		return fmt.Errorf("S3_BUCKET and S3_ENDPOINT must be set together")
	}
	if c.S3Enabled() {
		if c.S3AccessKey == "" || c.S3SecretKey == "" {
			return fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required when object storage is enabled")
		}
		if c.S3BackupInterval <= 0 {
			return fmt.Errorf("S3_BACKUP_INTERVAL must be greater than 0")
		}
		if c.S3BackupIncrInterval < 0 {
			return fmt.Errorf("S3_BACKUP_INCREMENTAL_INTERVAL must be 0 (disabled) or greater")
		}
		if c.S3BackupIncrInterval > 0 && c.S3BackupIncrInterval >= c.S3BackupInterval {
			return fmt.Errorf("S3_BACKUP_INCREMENTAL_INTERVAL must be less than S3_BACKUP_INTERVAL")
		}
	}

	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if value, ok := os.LookupEnv(key); ok {
		var i int64
		if _, err := fmt.Sscanf(value, "%d", &i); err == nil && i > 0 {
			return i
		}
	}
	return fallback
}
