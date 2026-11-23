package exec

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/wftest"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

type staticTargets struct{ p *pgxpool.Pool }

func (s staticTargets) Pool(context.Context, int64) (*pgxpool.Pool, error) { return s.p, nil }

type rig struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	svc  *workflow.Service
	h    *Handler
	acc  *wftest.FakeAccess
	apr  *wftest.FakeApprovals
	q    *wftest.FakeQueue
	uid  int64
	db   int64
}

func newRig(t *testing.T) *rig {
	pool := wftest.NewPool(t)
	uid, db := wftest.Seed(t, pool)
	r := &rig{t: t, ctx: context.Background(), pool: pool, acc: wftest.NewAccess(), apr: wftest.NewApprovals(), q: &wftest.FakeQueue{}, uid: uid, db: db}
	r.acc.Grant(uid, db, 2)
	repo := workflow.NewRepository(pool)
	r.svc = workflow.NewService(repo, r.acc, r.apr, r.q, nil)
	r.h = &Handler{Repo: repo, Targets: staticTargets{pool}, Access: r.acc, Approvals: r.apr, Queue: r.q,
		WorkerID: "w1", Lease: 2 * time.Second, MaxAttempts: 3, LockTimeout: 400 * time.Millisecond,
		RetryDelay: func(a int) time.Duration { return time.Duration(a) * 10 * time.Millisecond }}
	t.Cleanup(func() {
		for _, tb := range []string{"zz_n", "zz_f", "zz_r", "zz_u", "zz_l", "zz_big", "zz_ctl", "zz_p", "zz_q"} {
			_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+tb+" CASCADE")
		}
		_, _ = pool.Exec(context.Background(), "DROP FUNCTION IF EXISTS zz_chk(bigint,bigint)")
	})
	return r
}

func (r *rig) must(err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) mig(title, up, down string) int64 {
	r.t.Helper()
	m, err := r.svc.Create(r.ctx, r.uid, r.db, workflow.CreateRequest{Title: title, SQLUp: up, SQLDown: down})
	r.must(err)
	return m.ID
}

func (r *rig) batchMig(title, up, down string, size int) int64 {
	r.t.Helper()
	m, err := r.svc.Create(r.ctx, r.uid, r.db, workflow.CreateRequest{Title: title, SQLUp: up, SQLDown: down,
		IsBatchMode: true, BatchColumn: "id", BatchSize: size})
	r.must(err)
	return m.ID
}

// ready: submit -> approve -> queue; mengembalikan execution id.
func (r *rig) ready(migID int64) int64 {
	r.t.Helper()
	ex, err := r.svc.Submit(r.ctx, r.uid, r.db, migID)
	r.must(err)
	return r.approveQueue(ex.ID)
}

func (r *rig) readyRollback(migID int64) int64 {
	r.t.Helper()
	ex, err := r.svc.Rollback(r.ctx, r.uid, r.db, migID)
	r.must(err)
	return r.approveQueue(ex.ID)
}

func (r *rig) approveQueue(id int64) int64 {
	r.apr.Approve(id)
	r.must(r.svc.MarkApproved(r.ctx, id))
	_, err := r.svc.Enqueue(r.ctx, r.uid, r.db, id)
	r.must(err)
	return id
}

func (r *rig) run(execID int64) {
	r.t.Helper()
	r.must(r.h.Handle(r.ctx, execID))
}

func (r *rig) ex(id int64) workflow.Execution {
	r.t.Helper()
	e, err := r.h.Repo.GetExecution(r.ctx, id)
	r.must(err)
	return e
}

func (r *rig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	r.must(r.pool.QueryRow(r.ctx, sql, args...).Scan(&n))
	return n
}

func (r *rig) hist() int {
	return r.count(`SELECT count(*) FROM platform_migration_history WHERE database_id=$1`, r.db)
}
func (r *rig) locks() int { return r.count(`SELECT count(*) FROM platform_database_locks`) }
func (r *rig) tableExists(n string) bool {
	return r.count(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`, n) == 1
}

func par(n int, f func(i int)) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); f(i) }(i)
	}
	wg.Wait()
}
