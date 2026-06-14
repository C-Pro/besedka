package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	oshttp "net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"besedka/internal/assets"
	"besedka/internal/auth"
	"besedka/internal/backup"
	"besedka/internal/commands"
	"besedka/internal/config"
	"besedka/internal/filestore"
	"besedka/internal/http"
	"besedka/internal/images"
	"besedka/internal/objectstore"
	"besedka/internal/push"
	"besedka/internal/storage"
	"besedka/internal/ws"
	"besedka/static"

	"golang.org/x/sync/errgroup"
)

func run(ctx context.Context, addUser string) error {
	cfg, err := config.Load(addUser != "")
	if err != nil {
		return err
	}

	if addUser != "" {
		return commands.AddUser(addUser, cfg)
	}

	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid BaseURL: %w", err)
	}

	authConfig := auth.Config{
		Secret:        base64.StdEncoding.EncodeToString([]byte(cfg.AuthSecret)),
		TokenExpiry:   cfg.TokenExpiry,
		RPDisplayName: cfg.ChatName,
		RPID:          baseURL.Hostname(),
		RPOrigin:      cfg.BaseURL,
	}

	// Initialize object storage (optional). objClient is nil when disabled.
	objClient, err := objectstore.New(objectstore.Config{
		Endpoint:  cfg.S3Endpoint,
		Region:    cfg.S3Region,
		Bucket:    cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		PathStyle: cfg.S3PathStyle,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize object storage: %w", err)
	}

	// Recover the database from object storage if it is missing locally. Must
	// run before the database is opened.
	if objClient != nil {
		if recovered, err := backup.RecoverDBIfMissing(ctx, cfg.DBFile, cfg.AuthSecret, "backups/", objClient); err != nil {
			return fmt.Errorf("database recovery failed: %w", err)
		} else if recovered {
			slog.Info("recovered database from object storage", "path", cfg.DBFile)
		}
	}

	// Initialize FileStore, wrapping it with object-storage mirroring if enabled.
	local, err := filestore.NewLocalFileStore(cfg.UploadsPath)
	if err != nil {
		return fmt.Errorf("failed to initialize filestore: %w", err)
	}
	var fs filestore.FileStore = local
	var mirror *filestore.MirrorFileStore
	if objClient != nil {
		mirror = filestore.NewMirrorFileStore(local, objClient, "files/")
		fs = mirror
	}

	bbStorage, err := storage.NewBboltStorage(cfg.DBFile, []byte(cfg.AuthSecret), fs)
	if err != nil {
		return err
	}
	defer func() { _ = bbStorage.Close() }()

	// One-time backfill of thumbnails for existing images; must complete
	// before the HTTP servers start so thumbnail URLs are stable.
	if err := images.EnsureThumbnails(bbStorage); err != nil {
		return fmt.Errorf("thumbnail migration failed: %w", err)
	}

	authService, err := auth.NewAuthService(ctx, authConfig, bbStorage)
	if err != nil {
		return err
	}

	pushService, err := push.NewService(bbStorage)
	if err != nil {
		return fmt.Errorf("failed to initialize push service: %w", err)
	}

	hub := ws.NewHub(ctx, authService, bbStorage, pushService)

	// Load assets with substitution
	assetsFS, err := assets.Load(cfg.ChatName, static.Content)
	if err != nil {
		return fmt.Errorf("failed to load assets: %w", err)
	}

	adminServer := http.NewAdminServer(cfg, authService, hub, assetsFS)
	apiServer := http.NewAPIServer(cfg, authService, hub, bbStorage, pushService, cfg.APIAddr, assetsFS)

	g, gCtx := errgroup.WithContext(ctx)

	// Start object-storage background work: mirror upload workers (with backfill
	// of existing files) and the periodic database backup scheduler.
	if objClient != nil {
		g.Go(func() error {
			mirror.Start(gCtx)
			return nil
		})

		scheduler := backup.NewScheduler(bbStorage, objClient, "backups/", cfg.S3BackupInterval, int(cfg.S3BackupKeep))
		g.Go(func() error {
			if err := scheduler.Run(gCtx); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		})
	}

	// Start Admin Server
	g.Go(func() error {
		err := adminServer.Start()
		if err != nil && err != oshttp.ErrServerClosed {
			return err
		}
		return nil
	})

	// Start API Server
	g.Go(func() error {
		err := apiServer.Start()
		if err != nil && err != oshttp.ErrServerClosed {
			return err
		}
		return nil
	})

	// Wait for context cancellation (signal)
	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("Shutting down servers...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := adminServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("Admin server shutdown error", "error", err)
		}
		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("API server shutdown error", "error", err)
		}
		return nil
	})

	return g.Wait()
}

func main() {
	addUser := flag.String("add-user", "", "Username to create (creates user with random password and prints details)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, *addUser); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("Application stopped with error", "error", err)
		os.Exit(1)
	}
}
