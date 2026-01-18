package main

import (
	"besedka/internal/auth"
	"besedka/internal/commands"
	"besedka/internal/http"
	"besedka/internal/storage"
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

	if *addUser != "" {
		return commands.AddUser(*addUser)
	}

	authConfig := auth.Config{
		Secret:      base64.StdEncoding.EncodeToString([]byte("very-secure-secret-key-for-development-mode")),
		TokenExpiry: 24 * time.Hour,
	}

	dbFile := os.Getenv("BESEDKA_DB")
	if dbFile == "" {
		dbFile = "besedka.db"
	}
	bbStorage, err := storage.NewBboltStorage(dbFile)
	if err != nil {
		return err
	}
	defer func() { _ = bbStorage.Close() }()

	authService, err := auth.NewAuthService(ctx, authConfig, bbStorage)
	if err != nil {
		return err
	}

	adminAddr := os.Getenv("ADMIN_ADDR")
	if adminAddr == "" {
		adminAddr = "localhost:8081"
	}

	apiAddr := os.Getenv("API_ADDR")
	if apiAddr == "" {
		apiAddr = ":8080"
	}

	adminServer := http.NewAdminServer(authService, adminAddr)
	apiServer := http.NewAPIServer(authService, bbStorage, apiAddr)

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
