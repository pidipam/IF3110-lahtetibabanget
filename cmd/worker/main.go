package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/app"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approval"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/config"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/exec"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/obs"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/platformdb"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/queue"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func main() {
	cfg := config.Load()
	logger := obs.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	if err := run(cfg, logger); err != nil {
		logger.Error("worker stopped", "error", err.Error())
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
	shutdownTracer, err := obs.InitTracer(ctx, cfg.ServiceName+"-worker")
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
	targets, closeTargets, err := app.NewManagerTargets(db, encKey)
	if err != nil {
		return err
	}
	defer closeTargets()

	authorizer := authz.New(authz.NewPGStore(db))
	approvals := approval.NewService(db, authorizer)
	deps := app.NewWorkflowDeps(db, authorizer, approvals, targets)
	rawJobs := queue.New(db)
	jobs := obs.WrapQueue(rawJobs)

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

	newHandler := func(id string) *exec.Handler {
		return &exec.Handler{
			Repo:      deps.Repo,
			Targets:   deps.Targets,
			Access:    deps.Access,
			Approvals: deps.Approvals,
			Queue:     jobs,
			Log:       logger,
			WorkerID:  id,
			Lease:     lease,
		}
	}
	recovery := newHandler(workerID)

	if n, err := rawJobs.Recover(ctx); err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	} else if n > 0 {
		logger.Warn("job dengan lease kedaluwarsa ditandai gagal", "jumlah", n)
	}
	if _, err := recovery.RecoverStale(ctx); err != nil {
		return fmt.Errorf("recover eksekusi: %w", err)
	}
	go recovery.RunRecovery(ctx, recoveryEvery)
	go runJobRecovery(ctx, rawJobs, recoveryEvery, logger)

	slots := cfg.WorkerConcurrency
	if slots < 1 {
		slots = 1
	}
	logger.Info("worker berjalan", "worker", workerID, "concurrency", slots, "lease", lease.String(), "poll", poll.String())
	jobCtx, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJobs()
	var wg sync.WaitGroup
	for i := 1; i <= slots; i++ {
		id := fmt.Sprintf("%s/%d", workerID, i)
		h := newHandler(id)
		wg.Add(1)
		go func() {
			defer wg.Done()
			runSlot(ctx, jobCtx, h, rawJobs, id, lease, poll, cfg.WorkerMaxAttempts, logger)
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	drain := cfg.WorkerDrainTimeout
	logger.Info("worker berhenti: menunggu job yang sedang berjalan", "event", "worker.draining", "timeout", drain.String())
	select {
	case <-done:
		logger.Info("worker berhenti: semua job selesai", "event", "worker.drained")
	case <-time.After(drain):
		logger.Warn("worker berhenti: batas waktu habis, job dibatalkan dan akan dipulihkan", "event", "worker.drain_timeout")
		cancelJobs()
		<-done
	}
	return nil
}

func runSlot(ctx, jobCtx context.Context, handler *exec.Handler, jobs *queue.Store, slotID string, lease, poll time.Duration, maxAttempts int, logger *slog.Logger) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		job, err := jobs.Claim(ctx, slotID, lease)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("claim job gagal", "worker", slotID, "error", err.Error())
		} else if job != nil {
			process(jobCtx, handler, jobs, job, slotID, lease, maxAttempts, logger)
			continue
		}
		select {
		case <-ctx.Done():
			return
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

	traceCtx := obs.ContextWithTraceparent(runCtx, job.Traceparent)
	traceCtx, span := obs.StartSpan(traceCtx, "migration.execute",
		attribute.Int64("execution.id", job.ExecutionID),
		attribute.String("job.id", job.JobID),
		attribute.Int("job.attempt", job.Attempts))
	defer span.End()
	handleCtx := queue.WithTraceparent(traceCtx, obs.Traceparent(traceCtx))
	err := handler.Handle(handleCtx, job.ExecutionID)
	hbStop()
	<-hbDone

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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
