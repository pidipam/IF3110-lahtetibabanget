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

type Job struct {
	ID          int64
	JobID       string
	ExecutionID int64
	Attempts    int
	Traceparent string
}

type Store struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

var _ workflow.Queue = (*Store)(nil)

type traceparentKey struct{}

func WithTraceparent(ctx context.Context, traceparent string) context.Context {
	return context.WithValue(ctx, traceparentKey{}, traceparent)
}

func TraceparentFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceparentKey{}).(string)
	return v
}

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

func (s *Store) Heartbeat(ctx context.Context, jobID, worker string, lease time.Duration) (bool, error) {
	ct, err := s.db.Exec(ctx, `UPDATE platform_migration_jobs SET
		lease_until=now() + make_interval(secs => $3), updated_at=now()
		WHERE job_id=$1 AND status='running' AND locked_by=$2`, jobID, worker, lease.Seconds())
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

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
	b[6] = (b[6] & 0x0f) | 0x40 
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
