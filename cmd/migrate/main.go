package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"besedka/cmd/migrate/internal"
	"besedka/internal/config"
)

func main() {
	cfg, err := config.Load(false)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if cfg.AuthSecret == "" {
		log.Fatalf("config AUTH_SECRET is not set, meaning no encryption key is available for migration")
	}

	key := []byte(cfg.AuthSecret)

	// Backup db
	if _, err := os.Stat(cfg.DBFile); err == nil {
		dbBackupFile := fmt.Sprintf("%s.%d.tar.gz", cfg.DBFile, time.Now().Unix())
		log.Printf("Backing up database to %s...", dbBackupFile)
		cmd := exec.Command("tar", "-czf", dbBackupFile, cfg.DBFile)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Fatalf("failed to backup database: %v\nOutput: %s", err, out)
		}
	} else {
		log.Printf("Database file %s not found, skipping backup.", cfg.DBFile)
	}

	// Backup uploads
	if _, err := os.Stat(cfg.UploadsPath); err == nil {
		uploadsBackupFile := fmt.Sprintf("%s.%d.tar.gz", filepath.Clean(cfg.UploadsPath), time.Now().Unix())
		log.Printf("Backing up uploads to %s...", uploadsBackupFile)
		parentDir := filepath.Dir(filepath.Clean(cfg.UploadsPath))
		baseName := filepath.Base(filepath.Clean(cfg.UploadsPath))
		cmd := exec.Command("tar", "-czf", uploadsBackupFile, "-C", parentDir, baseName)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Fatalf("failed to backup uploads: %v\nOutput: %s", err, out)
		}
	} else {
		log.Printf("Uploads directory %s not found, skipping backup.", cfg.UploadsPath)
	}

	log.Println("Backups complete. Starting migration...")

	if err := internal.MigrateToEncryption(cfg.DBFile, cfg.UploadsPath, key); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	log.Println("Migration successful!")
}
