package queue

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/wftest"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

type queueRig struct {
	t      *testing.T
	ctx    context.Context
	pool   *pgxpool.Pool
	store  *Store
	execID int64
}

func newQueueRig(t *testing.T) *queueRig {
	t.Helper()
	pool := wftest.NewPool(t)
	uid, dbID := wftest.Seed(t, pool)
	repo := workflow.NewRepository(pool)
	m, err := repo.Create(context.Background(), dbID, uid, workflow.RevisionInput{
		Title: "job antrean", SQLUp: "SELECT 1", SQLDown: "SELECT 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := repo.RequestExecution(context.Background(), dbID, m.ID, uid, workflow.KindUp)
	if err != nil {
		t.Fatal(err)
	}
	return &queueRig{t: t, ctx: context.Background(), pool: pool, store: New(pool), execID: e.ID}
}

func (r *queueRig) count(query string, args ...any) int {
	r.t.Helper()
	return testutil.Count(r.t, r.pool, query, args...)
}

func TestEnqueueClaimAndFinish(t *testing.T) {
	r := newQueueRig(t)
	jobID, err := r.store.Enqueue(r.ctx, r.execID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if jobID == "" {
		t.Fatal("job_id kosong")
	}
	j, err := r.store.Claim(r.ctx, "worker-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if j == nil || j.JobID != jobID || j.ExecutionID != r.execID || j.Attempts != 1 {
		t.Fatalf("job tidak sesuai: %+v", j)
	}
	if next, err := r.store.Claim(r.ctx, "worker-b", time.Second); err != nil || next != nil {
		t.Fatalf("tidak boleh ada job lain: %+v %v", next, err)
	}
	if err := r.store.Finish(r.ctx, jobID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if got := r.count(`SELECT count(*) FROM platform_migration_jobs WHERE job_id=$1 AND status='done'`, jobID); got != 1 {
		t.Fatalf("status done: %d", got)
	}
}

func TestEnqueueMembuatJobBaruUntukRetry(t *testing.T) {
	r := newQueueRig(t)
	first, err := r.store.Enqueue(r.ctx, r.execID, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.store.Enqueue(r.ctx, r.execID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("retry harus membuat job_id baru: %s", first)
	}
	j1, err := r.store.Claim(r.ctx, "w", time.Second)
	if err != nil || j1 == nil {
		t.Fatalf("claim 1: %+v %v", j1, err)
	}
	j2, err := r.store.Claim(r.ctx, "w", time.Second)
	if err != nil || j2 == nil {
		t.Fatalf("claim 2: %+v %v", j2, err)
	}
	if j1.JobID == j2.JobID {
		t.Fatalf("dua worker mengambil job yang sama: %s", j1.JobID)
	}
}

func TestClaimMenungguRunAt(t *testing.T) {
	r := newQueueRig(t)
	if _, err := r.store.Enqueue(r.ctx, r.execID, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	j, err := r.store.Claim(r.ctx, "w", time.Second)
	if err != nil || j != nil {
		t.Fatalf("job belum jatuh tempo: %+v %v", j, err)
	}
	time.Sleep(400 * time.Millisecond)
	j, err = r.store.Claim(r.ctx, "w", time.Second)
	if err != nil || j == nil {
		t.Fatalf("job harus sudah tersedia: %+v %v", j, err)
	}
}

func TestHeartbeatRequeueDanRecover(t *testing.T) {
	r := newQueueRig(t)
	jobID, err := r.store.Enqueue(r.ctx, r.execID, 0)
	if err != nil {
		t.Fatal(err)
	}
	j, err := r.store.Claim(r.ctx, "worker-a", time.Second)
	if err != nil || j == nil {
		t.Fatalf("claim: %+v %v", j, err)
	}
	if ok, err := r.store.Heartbeat(r.ctx, jobID, "worker-a", time.Second); err != nil || !ok {
		t.Fatalf("heartbeat worker yang benar: %v %v", ok, err)
	}
	if ok, err := r.store.Heartbeat(r.ctx, jobID, "worker-b", time.Second); err != nil || ok {
		t.Fatalf("heartbeat worker lain harus gagal: %v %v", ok, err)
	}
	if err := r.store.Requeue(r.ctx, jobID, 0, "coba lagi"); err != nil {
		t.Fatal(err)
	}
	if got := r.count(`SELECT count(*) FROM platform_migration_jobs WHERE job_id=$1 AND status='pending'`, jobID); got != 1 {
		t.Fatalf("status pending setelah requeue: %d", got)
	}
	if _, err := r.store.Claim(r.ctx, "worker-a", time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(r.ctx, `UPDATE platform_migration_jobs SET lease_until=now() - interval '1 second' WHERE job_id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	n, err := r.store.Recover(r.ctx)
	if err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}
	if got := r.count(`SELECT count(*) FROM platform_migration_jobs WHERE job_id=$1 AND status='failed'`, jobID); got != 1 {
		t.Fatalf("status failed setelah recover: %d", got)
	}
}

func TestTraceparentIkutTersimpan(t *testing.T) {
	r := newQueueRig(t)
	const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ctx := WithTraceparent(r.ctx, tp)
	if _, err := r.store.Enqueue(ctx, r.execID, 0); err != nil {
		t.Fatal(err)
	}
	j, err := r.store.Claim(r.ctx, "w", time.Second)
	if err != nil || j == nil {
		t.Fatalf("claim: %+v %v", j, err)
	}
	if j.Traceparent != tp {
		t.Fatalf("traceparent: %q", j.Traceparent)
	}
}
