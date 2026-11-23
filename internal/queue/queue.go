// Package queue mengimplementasikan antrean migration M2 di atas PostgreSQL.
// Store memenuhi workflow.Queue; worker mengambil job dengan SKIP LOCKED dan
// memperpanjang lease lewat heartbeat. Eksekusi tetap menjadi tanggung jawab
// exec.Handler, bukan package ini.
package queue

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

var (
	ErrNotFound     = errors.New("job tidak ditemukan")
	ErrInvalidState = errors.New("status job tidak valid")
)

// Job adalah satu baris antrean yang sedang diproses worker.
type Job struct {
	ID          int64
	JobID       string
	ExecutionID int64
	Attempts    int
	Traceparent string
}

// Store menyimpan job di platform database yang sama dengan server.
type Store struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

var _ workflow.Queue = (*Store)(nil)

type traceparentKey struct{}

// WithTraceparent menyimpan nilai traceparent ke context agar ikut tersimpan
// di baris job. Paket obs (WP8) akan mengisi nilai ini dari propagasi OTel.
func WithTraceparent(ctx context.Context, traceparent string) context.Context {
	return context.WithValue(ctx, traceparentKey{}, traceparent)
}

// TraceparentFromContext mengembalikan nilai yang disimpan WithTraceparent.
func TraceparentFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceparentKey{}).(string)
	return v
}

// Enqueue membuat job baru untuk sebuah execution dan menjadwalkannya pada
// now()+delay. Job sengaja dibuat baru setiap kali dipanggil: retry internal
// exec.Handler dan recovery memanggil Enqueue lagi, sementara idempotensi
// permintaan pengguna sudah dijaga workflow.Service.
func (s *Store) Enqueue(ctx context.Context, executionID int64, delay time.Duration) (string, error) {
	if delay < 0 {
		delay = 0
	}
	jobID, err := newJobID()
	if err != nil {
		return "", err
	}
	err = s.db.QueryRow(ctx, `INSERT INTO platform_migration_jobs
		(job_id, execution_id, status, run_at, traceparent)
		VALUES ($1, $2, 'pending', now() + make_interval(secs => $3), $4)
		RETURNING job_id`, jobID, executionID, delay.Seconds(), TraceparentFromContext(ctx)).Scan(&jobID)
	if err != nil {
		return "", err
	}
	return jobID, nil
}

// Claim mengambil satu job yang sudah jatuh tempo. SKIP LOCKED membuat banyak
// worker aman mengambil job berbeda tanpa saling menunggu.
func (s *Store) Claim(ctx context.Context, worker string, lease time.Duration) (*Job, error) {
	if lease <= 0 {
		lease = 30 * time.Second
	}
	var j Job
	err := s.db.QueryRow(ctx, `UPDATE platform_migration_jobs SET
		status='running', locked_by=$1, locked_at=now(),
		lease_until=now() + make_interval(secs => $2),
		attempts=attempts+1, updated_at=now()
		WHERE id = (
			SELECT id FROM platform_migration_jobs
			WHERE status='pending' AND run_at <= now()
			ORDER BY run_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, job_id, execution_id, attempts, traceparent`, worker, lease.Seconds()).
		Scan(&j.ID, &j.JobID, &j.ExecutionID, &j.Attempts, &j.Traceparent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// Heartbeat memperpanjang lease job yang masih dipegang worker.
// false berarti job hilang (worker lain atau sudah selesai).
func (s *Store) Heartbeat(ctx context.Context, jobID, worker string, lease time.Duration) (bool, error) {
	ct, err := s.db.Exec(ctx, `UPDATE platform_migration_jobs SET
		lease_until=now() + make_interval(secs => $3), updated_at=now()
		WHERE job_id=$1 AND status='running' AND locked_by=$2`, jobID, worker, lease.Seconds())
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// Requeue menjadwalkan ulang job yang gagal pada level worker (error infra).
func (s *Store) Requeue(ctx context.Context, jobID string, delay time.Duration, msg string) error {
	if delay < 0 {
		delay = 0
	}
	ct, err := s.db.Exec(ctx, `UPDATE platform_migration_jobs SET
		status='pending', run_at=now() + make_interval(secs => $2),
		locked_by='', locked_at=NULL, lease_until=NULL,
		last_error=$3, updated_at=now()
		WHERE job_id=$1 AND status='running'`, jobID, delay.Seconds(), truncate(msg, 1000))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Finish menandai job selesai. status harus "done" atau "failed".
func (s *Store) Finish(ctx context.Context, jobID, status, msg string) error {
	if status != "done" && status != "failed" {
		return fmt.Errorf("%w: %s", ErrInvalidState, status)
	}
	ct, err := s.db.Exec(ctx, `UPDATE platform_migration_jobs SET
		status=$2, last_error=$3, locked_by='', locked_at=NULL, lease_until=NULL,
		finished_at=now(), updated_at=now()
		WHERE job_id=$1 AND status='running'`, jobID, status, truncate(msg, 1000))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Recover menandai job yang lease-nya kedaluwarsa sebagai failed. Penjadwalan
// ulang eksekusi dilakukan exec.Handler.RecoverStale, jadi tidak dibuat job
// pending ganda di sini. Mengembalikan jumlah job yang dipulihkan.
func (s *Store) Recover(ctx context.Context) (int64, error) {
	ct, err := s.db.Exec(ctx, `UPDATE platform_migration_jobs SET
		status='failed', last_error='worker lease expired',
		locked_by='', locked_at=NULL, lease_until=NULL,
		finished_at=now(), updated_at=now()
		WHERE status='running' AND (lease_until IS NULL OR lease_until < now())`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func newJobID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
