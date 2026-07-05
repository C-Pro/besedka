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
	"sync"
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

// cliOptions holds the parsed CLI-mode flags. When any is set, run() dispatches
// to the matching command and returns instead of booting the servers.
type cliOptions struct {
	addUser       string
	listUsers     bool
	deleteUser    string
	resetPassword string
	backup        bool
	shutdown      bool
	yes           bool
}

func run(ctx context.Context, cli cliOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch {
	case cli.addUser != "":
		return commands.AddUser(cli.addUser, cfg)
	case cli.listUsers:
		return commands.ListUsers(cfg)
	case cli.deleteUser != "":
		return commands.DeleteUser(cli.deleteUser, cli.yes, cfg)
	case cli.resetPassword != "":
		return commands.ResetPassword(cli.resetPassword, cfg)
	case cli.backup:
		return commands.Backup(cfg)
	case cli.shutdown:
		return commands.Shutdown(cfg)
	}

	// Own a cancel so the /api/shutdown endpoint can stop the whole process.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
	var scheduler *backup.Scheduler
	if objClient != nil {
		g.Go(func() error {
			mirror.Start(gCtx)
			return nil
		})

		scheduler = backup.NewScheduler(bbStorage, objClient, "backups/", cfg.S3BackupInterval, int(cfg.S3BackupKeep))
		g.Go(func() error {
			if err := scheduler.Run(gCtx); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		})
	}

	// Wire the admin server's backup/shutdown ops. shutdownErr records a backup
	// failure during graceful shutdown so run() returns non-nil and the process
	// exits non-zero.
	var (
		shutdownMu  sync.Mutex
		shutdownErr error
	)
	triggerExit := func(err error) {
		shutdownMu.Lock()
		if err != nil {
			shutdownErr = err
		}
		shutdownMu.Unlock()
		cancel()
	}

	var onBackup func(context.Context) error
	if scheduler != nil {
		onBackup = func(ctx context.Context) error { return scheduler.BackupOnce(ctx) }
	}

	adminServer.SetOps(
		onBackup,
		func(reqCtx context.Context) (bool, error) {
			// 1. Stop the primary server so no further writes reach the DB. It is
			// bounded: long-lived WebSocket connections won't drain, so we log and
			// proceed to the backup regardless.
			sctx, c := context.WithTimeout(context.Background(), 10*time.Second)
			defer c()
			if err := apiServer.Shutdown(sctx); err != nil {
				slog.Error("primary server shutdown during graceful shutdown", "error", err)
			}

			// 2. Take a final full backup, only when S3 is enabled, with retries.
			if scheduler == nil {
				return false, nil
			}
			const attempts = 3
			var err error
			for i := range attempts {
				if err = scheduler.BackupOnce(context.Background()); err == nil {
					return true, nil
				}
				slog.Error("shutdown backup attempt failed", "attempt", i+1, "error", err)
				if i < attempts-1 {
					time.Sleep(time.Duration(i+1) * 2 * time.Second)
				}
			}
			return false, fmt.Errorf("shutdown backup failed after %d attempts: %w", attempts, err)
		},
		triggerExit,
	)

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

	if err := g.Wait(); err != nil {
		return err
	}
	// Surface a backup failure from a graceful /api/shutdown so the process
	// exits non-zero.
	shutdownMu.Lock()
	defer shutdownMu.Unlock()
	return shutdownErr
}

func main() {
	addUser := flag.String("add-user", "", "Create a user (prints a registration setup link)")
	listUsers := flag.Bool("list-users", false, "List all users with their statuses")
	deleteUser := flag.String("delete-user", "", "Delete a user by username")
	resetPassword := flag.String("reset-password", "", "Reset a user's password by username (prints a new setup link)")
	backupFlag := flag.Bool("backup", false, "Trigger an out-of-schedule full backup (requires S3 backup enabled)")
	shutdown := flag.Bool("shutdown", false, "Stop the primary server, take a final backup, and stop the process")
	yes := flag.Bool("yes", false, "Skip confirmation prompts (e.g. for --delete-user)")
	flag.Parse()

	cli := cliOptions{
		addUser:       *addUser,
		listUsers:     *listUsers,
		deleteUser:    *deleteUser,
		resetPassword: *resetPassword,
		backup:        *backupFlag,
		shutdown:      *shutdown,
		yes:           *yes,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cli); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("Application stopped with error", "error", err)
		os.Exit(1)
	}
}
