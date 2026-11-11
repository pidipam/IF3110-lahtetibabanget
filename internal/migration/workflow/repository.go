package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository mengakses tabel platform_migration_*. Semua query yang menerima
// dbID selalu memfilter database_id agar tidak bocor lintas database.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Pool dipakai paket exec untuk lock/heartbeat.
func (r *Repository) Pool() *pgxpool.Pool { return r.pool }

const lockBase int64 = 7_310_110_000_000

func isCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}

const execCols = `id, migration_id, database_id, revision_number, kind, status, undoes_execution_id,
	requested_by, executed_sql, attempt, job_id, reject_reason, error_message,
	batches_committed, rows_committed, created_at, updated_at, queued_at, started_at, finished_at`

func scanExec(row pgx.Row) (Execution, error) {
	var e Execution
	var k, s string
	err := row.Scan(&e.ID, &e.MigrationID, &e.DatabaseID, &e.RevisionNumber, &k, &s, &e.UndoesExecution,
		&e.RequestedBy, &e.ExecutedSQL, &e.Attempt, &e.JobID, &e.RejectReason, &e.ErrorMessage,
		&e.BatchesCommitted, &e.RowsCommitted, &e.CreatedAt, &e.UpdatedAt, &e.QueuedAt, &e.StartedAt, &e.FinishedAt)
	e.Kind, e.Status = Kind(k), Status(s)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

const revCols = `migration_id, revision_number, sql_up, sql_down, is_irreversible, irreversible_reason,
	is_batch_mode, batch_column, batch_size, checksum, created_by, created_at`

func scanRev(row pgx.Row) (Revision, error) {
	var v Revision
	err := row.Scan(&v.MigrationID, &v.Number, &v.SQLUp, &v.SQLDown, &v.IsIrreversible, &v.IrreversibleReason,
		&v.IsBatchMode, &v.BatchColumn, &v.BatchSize, &v.Checksum, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

func insertRevision(ctx context.Context, tx pgx.Tx, migID int64, n int, userID int64, in RevisionInput) error {
	_, err := tx.Exec(ctx, `INSERT INTO platform_migration_revisions
		(migration_id, revision_number, sql_up, sql_down, is_irreversible, irreversible_reason,
		 is_batch_mode, batch_column, batch_size, checksum, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		migID, n, in.SQLUp, in.SQLDown, in.IsIrreversible, in.IrreversibleReason,
		in.IsBatchMode, in.BatchColumn, in.BatchSize, in.Checksum(), userID)
	return err
}

// Create membuat migration draft beserta revisi 1.
func (r *Repository) Create(ctx context.Context, dbID, userID int64, in RevisionInput) (Migration, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Migration{}, err
	}
	defer tx.Rollback(ctx)
	var m Migration
	err = tx.QueryRow(ctx, `INSERT INTO platform_migrations (database_id, title, created_by)
		VALUES ($1,$2,$3) RETURNING id, database_id, title, linear_order, current_revision, created_by, created_at, updated_at`,
		dbID, in.Title, userID).Scan(&m.ID, &m.DatabaseID, &m.Title, &m.LinearOrder, &m.CurrentRevision, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return Migration{}, err
	}
	if err := insertRevision(ctx, tx, m.ID, 1, userID, in); err != nil {
		return Migration{}, err
	}
	return m, tx.Commit(ctx)
}

const migCols = `id, database_id, title, linear_order, current_revision, created_by, created_at, updated_at`

func scanMig(row pgx.Row) (Migration, error) {
	var m Migration
	err := row.Scan(&m.ID, &m.DatabaseID, &m.Title, &m.LinearOrder, &m.CurrentRevision, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (r *Repository) Get(ctx context.Context, dbID, id int64) (Migration, error) {
	return scanMig(r.pool.QueryRow(ctx, `SELECT `+migCols+` FROM platform_migrations WHERE id=$1 AND database_id=$2`, id, dbID))
}

func (r *Repository) List(ctx context.Context, dbID int64) ([]Migration, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+migCols+` FROM platform_migrations WHERE database_id=$1 ORDER BY id`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Migration
	for rows.Next() {
		m, err := scanMig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) GetRevision(ctx context.Context, migID int64, n int) (Revision, error) {
	return scanRev(r.pool.QueryRow(ctx, `SELECT `+revCols+` FROM platform_migration_revisions WHERE migration_id=$1 AND revision_number=$2`, migID, n))
}

// Revise membuat revisi baru. Ditolak (ErrImmutable) bila ada eksekusi yang
// sudah melewati tahap approval. Eksekusi pending_review/approved dibatalkan
// dan nomor urutnya dilepas, sehingga approval harus diulang.
func (r *Repository) Revise(ctx context.Context, dbID, id, userID int64, in RevisionInput) (Migration, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Migration{}, err
	}
	defer tx.Rollback(ctx)
	var cur int
	err = tx.QueryRow(ctx, `SELECT current_revision FROM platform_migrations WHERE id=$1 AND database_id=$2 FOR UPDATE`, id, dbID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return Migration{}, ErrNotFound
	} else if err != nil {
		return Migration{}, err
	}
	var started int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform_migration_executions
		WHERE migration_id=$1 AND status NOT IN ('pending_review','approved','rejected','cancelled')`, id).Scan(&started); err != nil {
		return Migration{}, err
	}
	if started > 0 {
		return Migration{}, ErrImmutable
	}
	if _, err := tx.Exec(ctx, `UPDATE platform_migration_executions SET status='cancelled', reject_reason='revisi baru dibuat',
		finished_at=now(), updated_at=now() WHERE migration_id=$1 AND status IN ('pending_review','approved')`, id); err != nil {
		return Migration{}, err
	}
	if err := insertRevision(ctx, tx, id, cur+1, userID, in); err != nil {
		return Migration{}, err
	}
	m, err := scanMig(tx.QueryRow(ctx, `UPDATE platform_migrations SET title=$2, current_revision=$3, linear_order=NULL, updated_at=now()
		WHERE id=$1 RETURNING `+migCols, id, in.Title, cur+1))
	if err != nil {
		return Migration{}, err
	}
	return m, tx.Commit(ctx)
}

// Items menghitung posisi semua migration yang sudah punya urutan.
func (r *Repository) Items(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, dbID int64) ([]Item, error) {
	rows, err := q.Query(ctx, `SELECT m.id, m.linear_order,
		COALESCE((SELECT h.execution_type FROM platform_migration_history h
			WHERE h.migration_id=m.id ORDER BY h.execution_id DESC LIMIT 1), '')
		FROM platform_migrations m WHERE m.database_id=$1 AND m.linear_order IS NOT NULL ORDER BY m.linear_order`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var last string
		if err := rows.Scan(&it.ID, &it.Order, &last); err != nil {
			return nil, err
		}
		it.EverRun = last != ""
		it.Active = last == string(KindUp) || last == string(KindForward)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repository) ItemsFor(ctx context.Context, dbID int64) ([]Item, error) {
	return r.Items(ctx, r.pool, dbID)
}

// RequestExecution membuat eksekusi pending_review. Untuk kind=up, nomor urutan
// diberikan di sini (dengan advisory lock agar tanpa celah/duplikat).
func (r *Repository) RequestExecution(ctx context.Context, dbID, migID, userID int64, kind Kind) (Execution, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Execution{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockBase+dbID); err != nil {
		return Execution{}, err
	}
	m, err := scanMig(tx.QueryRow(ctx, `SELECT `+migCols+` FROM platform_migrations WHERE id=$1 AND database_id=$2 FOR UPDATE`, migID, dbID))
	if err != nil {
		return Execution{}, err
	}
	items, err := r.Items(ctx, tx, dbID)
	if err != nil {
		return Execution{}, err
	}
	var self *Item
	for i := range items {
		if items[i].ID == migID {
			self = &items[i]
		}
	}
	rev := m.CurrentRevision
	var undoes *int64
	switch kind {
	case KindUp:
		if self != nil && self.EverRun {
			return Execution{}, fmt.Errorf("%w: sudah pernah dijalankan, gunakan forward", ErrNotAllowed)
		}
		if m.LinearOrder == nil {
			if err := tx.QueryRow(ctx, `UPDATE platform_migrations SET linear_order =
				(SELECT COALESCE(MAX(linear_order),0)+1 FROM platform_migrations WHERE database_id=$1)
				WHERE id=$2 RETURNING linear_order`, dbID, migID).Scan(&m.LinearOrder); err != nil {
				return Execution{}, err
			}
		}
	case KindForward:
		if self == nil || !self.EverRun || self.Active {
			return Execution{}, fmt.Errorf("%w: forward hanya untuk migration yang sudah di-rollback", ErrNotAllowed)
		}
	case KindRollback:
		if self == nil || !self.Active {
			return Execution{}, ErrNotActive
		}
		last, _ := LastActive(items)
		if last.ID != migID {
			return Execution{}, ErrNotLastActive
		}
		var hid int64
		var hrev int
		if err := tx.QueryRow(ctx, `SELECT execution_id, revision_number FROM platform_migration_history
			WHERE migration_id=$1 ORDER BY execution_id DESC LIMIT 1`, migID).Scan(&hid, &hrev); err != nil {
			return Execution{}, err
		}
		v, err := scanRev(tx.QueryRow(ctx, `SELECT `+revCols+` FROM platform_migration_revisions WHERE migration_id=$1 AND revision_number=$2`, migID, hrev))
		if err != nil {
			return Execution{}, err
		}
		if v.IsIrreversible {
			return Execution{}, ErrIrreversible
		}
		undoes, rev = &hid, hrev
	default:
		return Execution{}, ErrNotAllowed
	}
	e, err := scanExec(tx.QueryRow(ctx, `INSERT INTO platform_migration_executions
		(migration_id, database_id, revision_number, kind, status, undoes_execution_id, requested_by)
		VALUES ($1,$2,$3,$4,'pending_review',$5,$6) RETURNING `+execCols, migID, dbID, rev, string(kind), undoes, userID))
	if err != nil {
		if isCode(err, "23505") {
			return Execution{}, ErrConflict
		}
		return Execution{}, err
	}
	return e, tx.Commit(ctx)
}

func (r *Repository) GetExecution(ctx context.Context, id int64) (Execution, error) {
	return scanExec(r.pool.QueryRow(ctx, `SELECT `+execCols+` FROM platform_migration_executions WHERE id=$1`, id))
}

func (r *Repository) GetExecutionScoped(ctx context.Context, dbID, id int64) (Execution, error) {
	return scanExec(r.pool.QueryRow(ctx, `SELECT `+execCols+` FROM platform_migration_executions WHERE id=$1 AND database_id=$2`, id, dbID))
}

func (r *Repository) ListExecutions(ctx context.Context, dbID int64) ([]Execution, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+execCols+` FROM platform_migration_executions WHERE database_id=$1 ORDER BY id DESC`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Execution
	for rows.Next() {
		e, err := scanExec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) LatestExecution(ctx context.Context, migID int64) (*Execution, error) {
	e, err := scanExec(r.pool.QueryRow(ctx, `SELECT `+execCols+` FROM platform_migration_executions WHERE migration_id=$1 ORDER BY id DESC LIMIT 1`, migID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func fromStatuses(to Status) []string {
	var out []string
	for from := range transitions {
		if CanTransition(from, to) == nil {
			out = append(out, string(from))
		}
	}
	return out
}

// SetStatus memindahkan status sesuai tabel transisi (satu sumber kebenaran).
func (r *Repository) SetStatus(ctx context.Context, id int64, to Status, reason string) (Execution, error) {
	e, err := scanExec(r.pool.QueryRow(ctx, `UPDATE platform_migration_executions SET status=$2, updated_at=now(),
		reject_reason = CASE WHEN $2 IN ('rejected','cancelled') THEN $4 ELSE reject_reason END,
		finished_at = CASE WHEN $2 IN ('rejected','cancelled') THEN now() ELSE finished_at END,
		queued_at = CASE WHEN $2='queued' THEN now() ELSE queued_at END
		WHERE id=$1 AND status = ANY($3) RETURNING `+execCols, id, string(to), fromStatuses(to), reason))
	if errors.Is(err, ErrNotFound) {
		if _, gerr := r.GetExecution(ctx, id); gerr == nil {
			return Execution{}, ErrInvalidTransition
		}
		return Execution{}, ErrNotFound
	}
	if err != nil {
		if isCode(err, "23505") {
			return Execution{}, ErrConflict
		}
		return Execution{}, err
	}
	if to == StatusRejected || to == StatusCancelled {
		r.releaseOrderIfNeverRun(ctx, e)
	}
	return e, nil
}

// releaseOrderIfNeverRun: migration yang ditolak/dibatalkan sebelum pernah jalan melepas nomornya.
func (r *Repository) releaseOrderIfNeverRun(ctx context.Context, e Execution) {
	if e.Kind != KindUp {
		return
	}
	_, _ = r.pool.Exec(ctx, `UPDATE platform_migrations SET linear_order=NULL, updated_at=now()
		WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM platform_migration_history WHERE migration_id=$1)
		AND NOT EXISTS (SELECT 1 FROM platform_migration_executions WHERE migration_id=$1
			AND status IN ('pending_review','approved','queued','running','partial','uncertain','partially_reverted'))`, e.MigrationID)
}

func (r *Repository) SetJobID(ctx context.Context, id int64, jobID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE platform_migration_executions SET job_id=$2, updated_at=now() WHERE id=$1`, id, jobID)
	return err
}

// ListHistory mengembalikan eksekusi sukses (urut waktu).
func (r *Repository) ListHistory(ctx context.Context, dbID int64) ([]HistoryEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT h.execution_id, h.migration_id, m.title, h.database_id, h.revision_number,
		h.execution_type, h.status, h.executed_by, h.executed_at, h.sql_executed, h.reverted_execution_id
		FROM platform_migration_history h JOIN platform_migrations m ON m.id=h.migration_id
		WHERE h.database_id=$1 ORDER BY h.executed_at, h.execution_id`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var h HistoryEntry
		var k, s string
		if err := rows.Scan(&h.ExecutionID, &h.MigrationID, &h.MigrationTitle, &h.DatabaseID, &h.RevisionNumber,
			&k, &s, &h.ExecutedBy, &h.ExecutedAt, &h.SQLExecuted, &h.RevertedExecutionID); err != nil {
			return nil, err
		}
		h.Kind, h.Status = Kind(k), Status(s)
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- dipakai worker ----

// Claim memindahkan eksekusi ke running secara atomik. prev adalah status sebelumnya.
// ok=false: pesan duplikat/sudah diproses worker lain. ErrConflict: database sedang ditahan eksekusi lain.
func (r *Repository) Claim(ctx context.Context, id int64) (e Execution, prev Status, ok bool, err error) {
	var p string
	row := r.pool.QueryRow(ctx, `UPDATE platform_migration_executions e SET status='running', attempt=e.attempt+1,
		started_at=COALESCE(e.started_at, now()), updated_at=now(), error_message=''
		FROM platform_migration_executions o
		WHERE e.id=$1 AND o.id=e.id AND e.status IN ('queued','partial','uncertain','partially_reverted')
		RETURNING e.id, e.migration_id, e.database_id, e.revision_number, e.kind, e.status, e.undoes_execution_id,
		e.requested_by, e.executed_sql, e.attempt, e.job_id, e.reject_reason, e.error_message,
		e.batches_committed, e.rows_committed, e.created_at, e.updated_at, e.queued_at, e.started_at, e.finished_at, o.status`, id)
	var k, s string
	err = row.Scan(&e.ID, &e.MigrationID, &e.DatabaseID, &e.RevisionNumber, &k, &s, &e.UndoesExecution,
		&e.RequestedBy, &e.ExecutedSQL, &e.Attempt, &e.JobID, &e.RejectReason, &e.ErrorMessage,
		&e.BatchesCommitted, &e.RowsCommitted, &e.CreatedAt, &e.UpdatedAt, &e.QueuedAt, &e.StartedAt, &e.FinishedAt, &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, "", false, nil
	}
	if err != nil {
		if isCode(err, "23505") {
			return e, "", false, ErrConflict
		}
		return e, "", false, err
	}
	e.Kind, e.Status = Kind(k), Status(s)
	return e, Status(p), true, nil
}

// MarkOutcome menulis status non-sukses (failed/partial/uncertain/partially_reverted/queued/cancelled).
// Memakai context.WithoutCancel agar hasil tetap tercatat saat ctx pembatalan.
func (r *Repository) MarkOutcome(ctx context.Context, id int64, to Status, msg string) error {
	ctx = context.WithoutCancel(ctx)
	ct, err := r.pool.Exec(ctx, `UPDATE platform_migration_executions SET status=$2, error_message=$3, updated_at=now(),
		finished_at = CASE WHEN $2 IN ('failed','cancelled') THEN now() ELSE finished_at END
		WHERE id=$1 AND status='running'`, id, string(to), truncate(msg, 1000))
	if err == nil && ct.RowsAffected() == 0 {
		return ErrInvalidTransition
	}
	return err
}

func (r *Repository) SetExecutedSQL(ctx context.Context, id int64, sql string) error {
	_, err := r.pool.Exec(ctx, `UPDATE platform_migration_executions SET executed_sql=$2, updated_at=now() WHERE id=$1`, id, sql)
	return err
}

func (r *Repository) SetProgress(ctx context.Context, id int64, batches int, rows int64) error {
	_, err := r.pool.Exec(context.WithoutCancel(ctx), `UPDATE platform_migration_executions
		SET batches_committed=$2, rows_committed=$3, updated_at=now() WHERE id=$1`, id, batches, rows)
	return err
}

// FinishSuccess: satu transaksi platform = tulis history + status success + lepas lock.
// Idempoten: bila history sudah ada (mis. setelah crash) tidak menggandakan.
func (r *Repository) FinishSuccess(ctx context.Context, e Execution, sql string) error {
	ctx = context.WithoutCancel(ctx)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO platform_migration_history
		(execution_id, migration_id, database_id, revision_number, execution_type, executed_by, sql_executed, reverted_execution_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (execution_id) DO NOTHING`,
		e.ID, e.MigrationID, e.DatabaseID, e.RevisionNumber, string(e.Kind), e.RequestedBy, sql, e.UndoesExecution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE platform_migration_executions SET status='success', executed_sql=$2, error_message='',
		finished_at=now(), updated_at=now() WHERE id=$1`, e.ID, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM platform_database_locks WHERE database_id=$1 AND execution_id=$2`, e.DatabaseID, e.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Backoff dipakai bersama oleh worker.
func Backoff(attempt int) time.Duration {
	d := time.Duration(1<<min(attempt, 6)) * time.Second
	return d
}
