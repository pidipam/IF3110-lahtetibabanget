// Package exec adalah worker eksekusi migration: klaim, lock, revalidasi,
// eksekusi (normal/batch), rollback/forward, pencatatan hasil, dan recovery.
package exec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

// Targets menyediakan pool ke database target (dbconn.Manager di aplikasi).
type Targets interface {
	Pool(ctx context.Context, dbID int64) (*pgxpool.Pool, error)
}

type Handler struct {
	Repo      *workflow.Repository
	Targets   Targets
	Access    workflow.Access
	Approvals workflow.Approvals
	Queue     workflow.Queue
	Log       *slog.Logger

	WorkerID    string
	Lease       time.Duration // default 30s
	MaxAttempts int           // default 3 (kegagalan sementara)
	StmtTimeout time.Duration // default 15m
	LockTimeout time.Duration // default 10s
	RetryDelay  func(attempt int) time.Duration

	once sync.Once
}

func (h *Handler) defaults() { h.once.Do(h.applyDefaults) }

func (h *Handler) applyDefaults() {
	if h.Lease == 0 {
		h.Lease = 30 * time.Second
	}
	if h.MaxAttempts == 0 {
		h.MaxAttempts = 3
	}
	if h.StmtTimeout == 0 {
		h.StmtTimeout = 15 * time.Minute
	}
	if h.LockTimeout == 0 {
		h.LockTimeout = 10 * time.Second
	}
	if h.RetryDelay == nil {
		h.RetryDelay = workflow.Backoff
	}
	if h.Log == nil {
		h.Log = slog.Default()
	}
	if h.WorkerID == "" {
		h.WorkerID = "worker-1"
	}
}

type outcome struct {
	class     Class
	err       error
	sql       string // SQL yang benar-benar dijalankan (untuk history)
	committed int    // batch committed (mode batch)
	reverting bool   // sedang kompensasi/rollback batch
}

func (h *Handler) locks() Locks { return Locks{Pool: h.Repo.Pool()} }

func (h *Handler) log(ctx context.Context, e workflow.Execution, event string, args ...any) {
	base := []any{"event", event, "database", e.DatabaseID, "migration", e.MigrationID, "execution", e.ID,
		"revision", e.RevisionNumber, "kind", string(e.Kind), "attempt", e.Attempt, "job", e.JobID, "worker", h.WorkerID}
	h.Log.InfoContext(ctx, event, append(base, args...)...)
}

// Handle memproses satu pesan antrean. Aman dipanggil ulang (pesan duplikat).
// Mengembalikan error hanya untuk kegagalan infrastruktur yang layak diulang oleh antrean.
func (h *Handler) Handle(ctx context.Context, execID int64) error {
	h.defaults()
	e, prev, ok, err := h.Repo.Claim(ctx, execID)
	if errors.Is(err, workflow.ErrConflict) {
		h.Log.InfoContext(ctx, "execution.deferred", "event", "execution.deferred", "execution", execID, "reason", "database busy")
		return h.defer_(ctx, execID, 5*time.Second)
	}
	if err != nil {
		return err
	}
	if !ok {
		h.Log.InfoContext(ctx, "execution.duplicate", "event", "execution.duplicate", "execution", execID)
		return nil
	}
	h.log(ctx, e, "execution.claimed", "previous_status", string(prev))

	got, err := h.locks().Acquire(ctx, e.DatabaseID, e.ID, h.WorkerID, h.Lease)
	if err != nil {
		_ = h.Repo.MarkOutcome(ctx, e.ID, prev, "gagal mengambil lock")
		return err
	}
	if !got {
		_ = h.Repo.MarkOutcome(ctx, e.ID, prev, "")
		h.log(ctx, e, "lock.busy")
		return h.defer_(ctx, e.ID, 5*time.Second)
	}
	h.log(ctx, e, "lock.acquired")

	runCtx, cancel := context.WithCancel(ctx)
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		h.locks().keepAlive(runCtx, e.DatabaseID, e.ID, h.WorkerID, h.Lease, func() {
			h.log(ctx, e, "lock.lost")
			cancel()
		})
	}()
	start := time.Now()
	out := h.run(runCtx, e, prev)
	cancel()
	<-hbDone
	h.log(ctx, e, "execution.result", "class", out.class.String(), "duration_ms", time.Since(start).Milliseconds(),
		"batches", out.committed, "error", errString(out.err))
	return h.finalize(ctx, e, prev, out)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (h *Handler) defer_(ctx context.Context, id int64, d time.Duration) error {
	_, err := h.Queue.Enqueue(ctx, id, d)
	return err
}

// finalize menerjemahkan hasil ke status akhir, lock, dan penjadwalan ulang.
func (h *Handler) finalize(ctx context.Context, e workflow.Execution, prev workflow.Status, out outcome) error {
	lk := h.locks()
	msg := errString(out.err)
	switch out.class {
	case Success:
		if out.reverting && e.Kind != workflow.KindRollback {
			// kompensasi batch selesai: eksekusi dibatalkan penuh, tidak masuk history
			if err := h.Repo.MarkOutcome(ctx, e.ID, workflow.StatusCancelled, "seluruh batch dikembalikan"); err != nil {
				return err
			}
			h.log(ctx, e, "execution.cancelled", "reason", "batches reverted")
			return lk.Release(ctx, e.DatabaseID, e.ID)
		}
		if err := h.Repo.FinishSuccess(ctx, e, out.sql); err != nil {
			return err // status masih running; recovery akan menandai uncertain lalu memeriksa bukti
		}
		h.log(ctx, e, "execution.committed")
		return nil
	case Uncertain:
		_ = h.Repo.MarkOutcome(ctx, e.ID, workflow.StatusUncertain, msg)
		_ = lk.Hold(ctx, e.DatabaseID, e.ID, "uncertain")
		h.log(ctx, e, "execution.uncertain")
		if e.Attempt < h.MaxAttempts {
			return h.defer_(ctx, e.ID, h.RetryDelay(e.Attempt))
		}
		return nil
	case Retryable:
		if e.Attempt < h.MaxAttempts {
			st, lockSt := workflow.StatusQueued, ""
			if out.committed > 0 || out.reverting {
				st, lockSt = holdStatus(out), holdLock(out)
			}
			if err := h.Repo.MarkOutcome(ctx, e.ID, st, msg); err != nil {
				return err
			}
			if lockSt == "" {
				_ = lk.Release(ctx, e.DatabaseID, e.ID)
			} else {
				_ = lk.Hold(ctx, e.DatabaseID, e.ID, lockSt)
			}
			h.log(ctx, e, "execution.retry_scheduled", "delay_ms", h.RetryDelay(e.Attempt).Milliseconds())
			return h.defer_(ctx, e.ID, h.RetryDelay(e.Attempt))
		}
		fallthrough
	default: // Permanent atau retry habis
		st := workflow.StatusFailed
		lockSt := ""
		if out.committed > 0 || out.reverting {
			st, lockSt = holdStatus(out), holdLock(out)
		}
		if err := h.Repo.MarkOutcome(ctx, e.ID, st, msg); err != nil {
			return err
		}
		if lockSt == "" {
			_ = lk.Release(ctx, e.DatabaseID, e.ID)
		} else {
			_ = lk.Hold(ctx, e.DatabaseID, e.ID, lockSt)
		}
		h.log(ctx, e, "execution.failed", "status", string(st))
		return nil
	}
}

func holdStatus(o outcome) workflow.Status {
	if o.reverting {
		return workflow.StatusPartiallyReverted
	}
	return workflow.StatusPartial
}

func holdLock(outcome) string { return "partial" }

func permMsg(format string, a ...any) outcome {
	return outcome{class: Permanent, err: fmt.Errorf(format, a...)}
}

func (h *Handler) run(ctx context.Context, e workflow.Execution, prev workflow.Status) outcome {
	rev, err := h.Repo.GetRevision(ctx, e.MigrationID, e.RevisionNumber)
	if err != nil {
		return outcome{class: Retryable, err: err}
	}
	if o, bad := h.revalidate(ctx, e, rev, prev); bad {
		return o
	}
	pool, err := h.Targets.Pool(ctx, e.DatabaseID)
	if err != nil {
		return outcome{class: Retryable, err: fmt.Errorf("database target tidak tersedia: %w", err)}
	}
	if err := ensureMeta(ctx, pool); err != nil {
		return outcome{class: Classify(err, StageRun), err: fmt.Errorf("menyiapkan dbmp_meta: %w", err)}
	}
	isBatch := rev.IsBatchMode
	switch {
	case prev == workflow.StatusPartiallyReverted:
		src := e.ID
		if e.Kind == workflow.KindRollback && e.UndoesExecution != nil {
			src = *e.UndoesExecution
		}
		return h.revertRanges(ctx, e, rev, pool, src)
	case e.Kind == workflow.KindRollback && isBatch && e.UndoesExecution != nil:
		return h.revertRanges(ctx, e, rev, pool, *e.UndoesExecution)
	case isBatch:
		return h.runBatches(ctx, e, rev, pool)
	default:
		return h.runNormal(ctx, e, rev, pool, prev)
	}
}

// revalidate: status, checksum, approval, hak aksi pemohon, posisi, reversibilitas.
func (h *Handler) revalidate(ctx context.Context, e workflow.Execution, rev workflow.Revision, prev workflow.Status) (outcome, bool) {
	in := workflow.RevisionInput{SQLUp: rev.SQLUp, SQLDown: rev.SQLDown, IsIrreversible: rev.IsIrreversible,
		IsBatchMode: rev.IsBatchMode, BatchColumn: rev.BatchColumn, BatchSize: rev.BatchSize}
	if in.Checksum() != rev.Checksum {
		return permMsg("revalidasi gagal: checksum revisi tidak cocok"), true
	}
	if err := h.Access.Can(ctx, e.RequestedBy, e.DatabaseID, true); err != nil {
		return permMsg("revalidasi gagal: pemohon tidak lagi memiliki akses edit"), true
	}
	// Approval diperiksa untuk SEMUA jalur, termasuk resume dan pembatalan batch:
	// keduanya tetap tunduk pada approval (spesifikasi). Pembatalan memakai SQL down
	// yang sudah disetujui bersama migration, jadi tidak perlu ronde approval baru.
	ok, err := h.Approvals.Satisfied(ctx, e.ID)
	if err != nil {
		return outcome{class: Retryable, err: err}, true
	}
	if !ok {
		return permMsg("revalidasi gagal: approval tidak lengkap"), true
	}
	if prev != workflow.StatusQueued {
		return outcome{}, false // lanjutan: posisi sudah lolos saat dimulai; database ditahan lock
	}
	items, err := h.Repo.ItemsFor(ctx, e.DatabaseID)
	if err != nil {
		return outcome{class: Retryable, err: err}, true
	}
	if err := workflow.CheckRun(e.Kind, e.MigrationID, !rev.IsIrreversible, items); err != nil {
		return permMsg("revalidasi gagal: %v", err), true
	}
	return outcome{}, false
}

func (h *Handler) beginTx(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d; SET LOCAL lock_timeout = %d",
		h.StmtTimeout.Milliseconds(), h.LockTimeout.Milliseconds()), pgx.QueryExecModeSimpleProtocol); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, err
	}
	return tx, nil
}

func sqlFor(e workflow.Execution, rev workflow.Revision) string {
	if e.Kind == workflow.KindRollback {
		return rev.SQLDown
	}
	return rev.SQLUp
}

// runNormal: satu transaksi di target; marker ditulis bersama SQL.
func (h *Handler) runNormal(ctx context.Context, e workflow.Execution, rev workflow.Revision, pool *pgxpool.Pool, prev workflow.Status) outcome {
	sql := sqlFor(e, rev)
	if prev == workflow.StatusUncertain {
		ok, err := hasMarker(ctx, pool, e.ID)
		if err != nil {
			return outcome{class: Retryable, err: fmt.Errorf("memeriksa marker: %w", err)}
		}
		if ok { // bukti commit ada: jangan jalankan ulang
			h.log(ctx, e, "execution.marker_found")
			return outcome{class: Success, sql: sql}
		}
	}
	if err := h.Repo.SetExecutedSQL(ctx, e.ID, sql); err != nil {
		return outcome{class: Retryable, err: err}
	}
	tx, err := h.beginTx(ctx, pool)
	if err != nil {
		return outcome{class: Classify(err, StageRun), err: err}
	}
	rb := func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }
	h.log(ctx, e, "sql.start")
	t0 := time.Now()
	ct, err := tx.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol) // multi-statement
	if err != nil {
		rb()
		h.log(ctx, e, "sql.rollback", "sqlstate", SQLState(err), "duration_ms", time.Since(t0).Milliseconds())
		return outcome{class: Classify(err, StageRun), err: fmt.Errorf("SQL gagal (SQLSTATE %s): %s", SQLState(err), errMsg(err))}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dbmp_meta.execution_marker (execution_id, kind) VALUES ($1,$2)
		ON CONFLICT (execution_id) DO NOTHING`, e.ID, string(e.Kind)); err != nil {
		rb()
		return outcome{class: Classify(err, StageRun), err: err}
	}
	if err := tx.Commit(ctx); err != nil {
		h.log(ctx, e, "sql.commit_unknown", "error", err.Error())
		return outcome{class: Classify(err, StageCommit), err: fmt.Errorf("hasil commit tidak pasti: %w", err)}
	}
	h.log(ctx, e, "sql.commit", "rows", ct.RowsAffected(), "duration_ms", time.Since(t0).Milliseconds())
	return outcome{class: Success, sql: sql}
}

func errMsg(err error) string {
	var m string
	if pe := pgErr(err); pe != "" {
		m = pe
	} else {
		m = err.Error()
	}
	return m
}
