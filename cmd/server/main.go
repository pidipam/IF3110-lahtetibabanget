package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/app"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/config"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/platformdb"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := platformdb.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := platformdb.Migrate(ctx, db); err != nil {
		return err
	}
	a, err := app.New(app.Options{DB: db, Session: auth.SessionConfig{IdleTimeout: cfg.IdleTimeout, AbsoluteTimeout: cfg.AbsoluteTimeout, RotateInterval: cfg.RotateInterval, GracePeriod: cfg.GracePeriod}, Secure: cfg.Production, Logger: logger})
	if err != nil {
		return err
	}
	created, err := app.SeedAdmin(ctx, a.Users, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return err
	}
	if created {
		logger.Info("admin pertama dibuat", "username", cfg.AdminUsername)
	}
	go cleanupSessions(ctx, a.Sessions, logger)
	srv := &http.Server{Addr: cfg.Addr, Handler: a, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	logger.Info("server berjalan", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func cleanupSessions(ctx context.Context, store *auth.SessionStore, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := store.DeleteExpired(ctx); err != nil {
				logger.Error("cleanup session gagal", "err", err)
			}
		}
	}
}