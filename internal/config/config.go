package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	DBFile      string
	AdminAddr   string
	APIAddr     string
	BaseURL     string
	UploadsPath string
	AuthSecret  string
	TokenExpiry time.Duration
}

func Load(cliMode bool) (*Config, error) {
	tokenExpiry, err := time.ParseDuration(getEnv("TOKEN_EXPIRY", "24h"))
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DBFile:      getEnv("BESEDKA_DB", "besedka.db"),
		AdminAddr:   getEnv("ADMIN_ADDR", "localhost:8081"),
		APIAddr:     getEnv("API_ADDR", ":8080"),
		BaseURL:     getEnv("BASE_URL", "http://localhost:8080"),
		UploadsPath: getEnv("UPLOADS_PATH", "uploads"),
		AuthSecret:  os.Getenv("AUTH_SECRET"),
		TokenExpiry: tokenExpiry,
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

	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
