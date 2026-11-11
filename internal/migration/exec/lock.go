package exec

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Locks mengelola tabel platform_database_locks (lease + heartbeat lintas worker).
type Locks struct{ Pool *pgxpool.Pool }

// Acquire: true bila lock dipegang oleh eksekusi ini. Lock milik eksekusi lain
// TIDAK dapat direbut (termasuk yang lease-nya habis); eksekusi itu harus
// dipulihkan dulu (RecoverStale) agar tidak ada dua migration pada satu database.
func (l Locks) Acquire(ctx context.Context, dbID, execID int64, worker string, lease time.Duration) (bool, error) {
	var got int64
	err := l.Pool.QueryRow(ctx, `INSERT INTO platform_database_locks (database_id, execution_id, worker_id, lease_until)
		VALUES ($1,$2,$3, now() + make_interval(secs => $4))
		ON CONFLICT (database_id) DO UPDATE SET worker_id=$3, heartbeat_at=now(),
			lease_until = now() + make_interval(secs => $4), status='locked'
		WHERE platform_database_locks.execution_id=$2
		RETURNING database_id`, dbID, execID, worker, lease.Seconds()).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Heartbeat memperpanjang lease. false = lock hilang (worker harus berhenti).
func (l Locks) Heartbeat(ctx context.Context, dbID, execID int64, worker string, lease time.Duration) (bool, error) {
	ct, err := l.Pool.Exec(ctx, `UPDATE platform_database_locks SET heartbeat_at=now(),
		lease_until = now() + make_interval(secs => $4)
		WHERE database_id=$1 AND execution_id=$2 AND worker_id=$3`, dbID, execID, worker, lease.Seconds())
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// Hold menandai lock tetap ditahan (partial/uncertain/partially_reverted) tanpa lease aktif.
func (l Locks) Hold(ctx context.Context, dbID, execID int64, status string) error {
	_, err := l.Pool.Exec(context.WithoutCancel(ctx), `UPDATE platform_database_locks SET status=$3, lease_until=now()
		WHERE database_id=$1 AND execution_id=$2`, dbID, execID, status)
	return err
}

func (l Locks) Release(ctx context.Context, dbID, execID int64) error {
	_, err := l.Pool.Exec(context.WithoutCancel(ctx), `DELETE FROM platform_database_locks WHERE database_id=$1 AND execution_id=$2`, dbID, execID)
	return err
}

// keepAlive menjalankan heartbeat tiap lease/3 sampai ctx selesai; bila lock hilang memanggil lost().
func (l Locks) keepAlive(ctx context.Context, dbID, execID int64, worker string, lease time.Duration, lost func()) {
	t := time.NewTicker(lease / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ok, err := l.Heartbeat(ctx, dbID, execID, worker, lease)
			if err == nil && !ok {
				lost()
				return
			}
		}
	}
}
