// Package wftest berisi alat bantu uji integrasi workflow/exec (hanya dipakai file _test).
package wftest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

// NewPool memakai harness M1 (testutil.NewTestDB): menjalankan semua migrasi platform
// (termasuk 005), lalu TRUNCATE semua tabel. Skema dbmp_meta (sisi target) dibuang
// karena pada uji ini database uji juga berperan sebagai database target.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testutil.NewTestDB(t)
	if _, err := pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS dbmp_meta CASCADE`); err != nil {
		t.Fatal(err)
	}
	return pool
}

// Seed membuat satu pengguna dan satu koneksi database; mengembalikan (userID, dbID).
func Seed(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
	t.Helper()
	u := testutil.SeedUser(t, pool, "wftest_user", "Password123!", false)
	return u, testutil.SeedConnection(t, pool).ID
}

// ---- fakes ----

type FakeAccess struct {
	mu sync.Mutex
	m  map[[2]int64]int // 1=view 2=edit
}

func NewAccess() *FakeAccess { return &FakeAccess{m: map[[2]int64]int{}} }

func (a *FakeAccess) Grant(uid, db int64, level int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.m[[2]int64{uid, db}] = level
}

func (a *FakeAccess) Can(_ context.Context, uid, db int64, edit bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	lv := a.m[[2]int64{uid, db}]
	if lv == 0 || (edit && lv < 2) {
		return workflow.ErrForbidden
	}
	return nil
}

type FakeApprovals struct {
	mu       sync.Mutex
	Opened   []int64
	approved map[int64]bool
	FailOpen bool
}

func NewApprovals() *FakeApprovals { return &FakeApprovals{approved: map[int64]bool{}} }

func (a *FakeApprovals) Open(_ context.Context, id, _ int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.FailOpen {
		return fmt.Errorf("approval down")
	}
	a.Opened = append(a.Opened, id)
	return nil
}
func (a *FakeApprovals) Satisfied(_ context.Context, id int64) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.approved[id], nil
}
func (a *FakeApprovals) Approve(id int64) { a.mu.Lock(); a.approved[id] = true; a.mu.Unlock() }
func (a *FakeApprovals) Revoke(id int64)  { a.mu.Lock(); delete(a.approved, id); a.mu.Unlock() }

type Job struct {
	ExecID int64
	Delay  time.Duration
}

type FakeQueue struct {
	mu   sync.Mutex
	Jobs []Job
	Fail bool
}

func (q *FakeQueue) Enqueue(_ context.Context, id int64, d time.Duration) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.Fail {
		return "", fmt.Errorf("queue down")
	}
	q.Jobs = append(q.Jobs, Job{id, d})
	return fmt.Sprintf("job-%d", len(q.Jobs)), nil
}
func (q *FakeQueue) Count() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.Jobs) }
func (q *FakeQueue) Last() Job  { q.mu.Lock(); defer q.mu.Unlock(); return q.Jobs[len(q.Jobs)-1] }
