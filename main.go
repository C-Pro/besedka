package main

import (
	"besedka/internal/auth"
	"besedka/internal/commands"
	"besedka/internal/config"
	"besedka/internal/http"
	"besedka/internal/storage"
	"besedka/internal/ws"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"log"
	oshttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

func run(ctx context.Context) error {
	addUser := flag.String("add-user", "", "Username to create (creates user with random password and prints details)")
	flag.Parse()

	cfg, err := config.Load(*addUser != "")
	if err != nil {
		return err
	}

	if *addUser != "" {
		return commands.AddUser(*addUser, cfg)
	}

	authConfig := auth.Config{
		Secret:      base64.StdEncoding.EncodeToString([]byte(cfg.AuthSecret)),
		TokenExpiry: cfg.TokenExpiry,
	}

	bbStorage, err := storage.NewBboltStorage(cfg.DBFile)
	if err != nil {
		return err
	}
	defer func() { _ = bbStorage.Close() }()

	authService, err := auth.NewAuthService(ctx, authConfig, bbStorage)
	if err != nil {
		return err
	}

	hub := ws.NewHub(authService, bbStorage)

	adminServer := http.NewAdminServer(authService, hub, cfg.AdminAddr)
	apiServer := http.NewAPIServer(authService, hub, cfg.APIAddr)

	g, gCtx := errgroup.WithContext(ctx)

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
		log.Println("Shutting down servers...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := adminServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Admin server shutdown error: %v", err)
		}
		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("API server shutdown error: %v", err)
		}
		return nil
	})

	return g.Wait()
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Application error: %v", err)
	}
}
