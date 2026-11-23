package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/app"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/config"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/obs"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/platformdb"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/queue"
)

func main() {
	cfg := config.Load()
	logger := obs.NewLogger(cfg.LogLevel)
	if err := run(cfg, logger); err != nil {
		logger.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	encKey, err := dbconn.ParseKey(cfg.EncryptionKey)
	if err != nil {
		return fmt.Errorf("APP_ENCRYPTION_KEY tidak valid: %w (gunakan openssl rand -base64 32)", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTracer, err := obs.InitTracer(ctx, cfg.ServiceName+"-server")
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracer(shutdownCtx)
	}()
	db, err := platformdb.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := platformdb.Migrate(ctx, db); err != nil {
		return err
	}
	jobQueue := obs.WrapQueue(queue.New(db))
	a, err := app.New(app.Options{DB: db, Session: auth.SessionConfig{IdleTimeout: cfg.IdleTimeout, AbsoluteTimeout: cfg.AbsoluteTimeout, RotateInterval: cfg.RotateInterval, GracePeriod: cfg.GracePeriod}, Secure: cfg.Production, Logger: logger, EncryptionKey: encKey, Queue: jobQueue})
	if err != nil {
		return err
	}
	defer a.Close()
	created, err := app.SeedAdmin(ctx, a.Users, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return err
	}
	if created {
		logger.Info("admin pertama dibuat", "username", cfg.AdminUsername)
	}

	if n, err := a.RecoverMigrations(ctx); err != nil {
		return err
	} else if n > 0 {
		logger.Warn("migration tertinggal berstatus running ditandai gagal", "jumlah", n)
	}

	a.StartWorkflowRecovery(ctx)
	go cleanupSessions(ctx, a.Sessions, logger)
	srv := &http.Server{Addr: cfg.Addr, Handler: obs.Middleware(a), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
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
