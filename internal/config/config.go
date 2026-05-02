package config

import (
	"fmt"
	"os"
	"time"
)

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
}

func Load(cliMode bool) (*Config, error) {
	tokenExpiry, err := time.ParseDuration(getEnv("TOKEN_EXPIRY", "24h"))
	if err != nil {
		return nil, err
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
	}

	if err := cfg.Validate(cliMode); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate(cliMode bool) error {
	if c.AuthSecret == "" && !cliMode {
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
