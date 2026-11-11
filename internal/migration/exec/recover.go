package exec

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func asPg(err error, target **pgconn.PgError) bool { return errors.As(err, target) }

// RecoverStale menandai eksekusi 'running' yang lease lock-nya sudah habis
// (worker mati) sebagai 'uncertain', menahan lock, lalu menjadwalkan ulang.
// Handler kemudian memeriksa marker/checkpoint sebelum melanjutkan.
func (h *Handler) RecoverStale(ctx context.Context) ([]int64, error) {
	h.defaults()
	rows, err := h.Repo.Pool().Query(ctx, `UPDATE platform_migration_executions e
		SET status='uncertain', error_message='worker berhenti; hasil belum pasti', updated_at=now()
		WHERE e.status='running'
		AND NOT EXISTS (SELECT 1 FROM platform_database_locks l WHERE l.execution_id=e.id AND l.lease_until > now())
		AND e.updated_at < now() - interval '20 seconds'
		RETURNING e.id, e.database_id`)
	if err != nil {
		return nil, err
	}
	type pair struct{ id, db int64 }
	var ps []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.db); err != nil {
			rows.Close()
			return nil, err
		}
		ps = append(ps, p)
	}
	rows.Close()
	var ids []int64
	for _, p := range ps {
		_, _ = h.Repo.Pool().Exec(ctx, `INSERT INTO platform_database_locks (database_id, execution_id, worker_id, lease_until, status)
			VALUES ($1,$2,'recovery', now(), 'uncertain')
			ON CONFLICT (database_id) DO UPDATE SET status='uncertain', lease_until=now() WHERE platform_database_locks.execution_id=$2`, p.db, p.id)
		h.Log.WarnContext(ctx, "execution.recovered", "event", "execution.recovered", "execution", p.id, "database", p.db)
		if _, err := h.Queue.Enqueue(ctx, p.id, time.Second); err != nil {
			return ids, err
		}
		ids = append(ids, p.id)
	}
	return ids, nil
}

// RunRecovery menjalankan RecoverStale periodik sampai ctx selesai (panggil di cmd/worker).
func (h *Handler) RunRecovery(ctx context.Context, every time.Duration) {
	h.defaults()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := h.RecoverStale(ctx); err != nil && ctx.Err() == nil {
			h.Log.ErrorContext(ctx, "recovery.error", "event", "recovery.error", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
