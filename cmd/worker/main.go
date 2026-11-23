package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/app"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approval"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/config"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/exec"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/platformdb"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/queue"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg := config.Load()
	encKey, err := dbconn.ParseKey(cfg.EncryptionKey)
	if err != nil {
		return fmt.Errorf("APP_ENCRYPTION_KEY tidak valid: %w (gunakan openssl rand -base64 32)", err)
	}
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
	targets, closeTargets, err := app.NewManagerTargets(db, encKey)
	if err != nil {
		return err
	}
	defer closeTargets()

	authorizer := authz.New(authz.NewPGStore(db))
	approvals := approval.NewService(db, authorizer)
	deps := app.NewWorkflowDeps(db, authorizer, approvals, targets)
	jobs := queue.New(db)

	lease := cfg.WorkerLease
	if lease < 3*time.Second {
		lease = 3 * time.Second
	}
	poll := cfg.WorkerPollInterval
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	recoveryEvery := cfg.WorkerRecoveryInterval
	if recoveryEvery <= 0 {
		recoveryEvery = 15 * time.Second
	}
	workerID := cfg.WorkerID
	if workerID == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "worker"
		}
		workerID = fmt.Sprintf("%s-%d", host, os.Getpid())
	}

	handler := &exec.Handler{
		Repo:      deps.Repo,
		Targets:   deps.Targets,
		Access:    deps.Access,
		Approvals: deps.Approvals,
		Queue:     jobs,
		Log:       logger,
		WorkerID:  workerID,
		Lease:     lease,
	}

	if n, err := jobs.Recover(ctx); err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	} else if n > 0 {
		logger.Warn("job dengan lease kedaluwarsa ditandai gagal", "jumlah", n)
	}
	if _, err := handler.RecoverStale(ctx); err != nil {
		return fmt.Errorf("recover eksekusi: %w", err)
	}
	go handler.RunRecovery(ctx, recoveryEvery)
	go runJobRecovery(ctx, jobs, recoveryEvery, logger)

	logger.Info("worker berjalan", "worker", workerID, "lease", lease.String(), "poll", poll.String())
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		job, err := jobs.Claim(ctx, workerID, lease)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Error("claim job gagal", "error", err.Error())
		} else if job != nil {
			process(ctx, handler, jobs, job, workerID, lease, cfg.WorkerMaxAttempts, logger)
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func runJobRecovery(ctx context.Context, jobs *queue.Store, every time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := jobs.Recover(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Error("recover job gagal", "error", err.Error())
			} else if n > 0 {
				logger.Warn("job lease kedaluwarsa dipulihkan", "jumlah", n)
			}
		}
	}
}

func process(ctx context.Context, handler *exec.Handler, jobs *queue.Store, job *queue.Job, workerID string, lease time.Duration, maxAttempts int, logger *slog.Logger) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	hbCtx, hbStop := context.WithCancel(context.Background())
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		t := time.NewTicker(lease / 3)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				ok, err := jobs.Heartbeat(context.Background(), job.JobID, workerID, lease)
				if err != nil {
					logger.Error("heartbeat job gagal", "job", job.JobID, "error", err.Error())
				}
				if !ok {
					logger.Warn("lease job hilang, eksekusi dihentikan", "job", job.JobID)
					cancel()
					return
				}
			}
		}
	}()

	handleCtx := queue.WithTraceparent(runCtx, job.Traceparent)
	err := handler.Handle(handleCtx, job.ExecutionID)
	hbStop()
	<-hbDone

	if err != nil {
		if ctx.Err() != nil {
			logger.Warn("worker berhenti saat memproses job", "job", job.JobID, "execution", job.ExecutionID)
			return
		}
		if job.Attempts < maxAttempts {
			delay := workflow.Backoff(job.Attempts)
			if e := jobs.Requeue(context.Background(), job.JobID, delay, err.Error()); e != nil {
				logger.Error("requeue job gagal", "job", job.JobID, "error", e.Error())
			}
			logger.Warn("job dijadwalkan ulang", "job", job.JobID, "attempt", job.Attempts, "delay", delay.String(), "error", err.Error())
		} else if e := jobs.Finish(context.Background(), job.JobID, "failed", err.Error()); e != nil && !errors.Is(e, queue.ErrNotFound) {
			logger.Error("gagal menandai job failed", "job", job.JobID, "error", e.Error())
		}
		return
	}
	if e := jobs.Finish(context.Background(), job.JobID, "done", ""); e != nil && !errors.Is(e, queue.ErrNotFound) {
		logger.Error("gagal menandai job done", "job", job.JobID, "error", e.Error())
	}
}
