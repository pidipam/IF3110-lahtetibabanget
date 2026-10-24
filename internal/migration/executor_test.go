package migration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

type execEnv struct {
	e      *Executor
	repo   *Repository
	db     *pgxpool.Pool
	target *pgxpool.Pool
	uid    int64
}

func setupExec(t *testing.T) *execEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test dilewati pada -short")
	}
	url := testutil.RequireEnv(t, "TARGET_DB_URL")
	repo, db, uid := testRepo(t)
	ctx := context.Background()
	target, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.Close)
	const drop = "DROP TABLE IF EXISTS zz_mig_t1, zz_mig_t2, zz_mig_t3 CASCADE"
	t.Cleanup(func() { target.Exec(context.Background(), drop) }) // jalan sebelum target.Close
	if _, err := target.Exec(ctx, drop); err != nil {
		t.Fatal(err)
	}
	targets, err := NewStaticTargets(ctx, 1, "target-test", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(targets.Close)
	return &execEnv{e: NewExecutor(db, repo, targets, 0), repo: repo, db: db, target: target, uid: uid}
}

func (x *execEnv) draft(t *testing.T, sql string) *Migration {
	t.Helper()
	m, err := x.repo.Create(context.Background(), 1, x.uid, "uji", sql)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (x *execEnv) tableExists(t *testing.T, name string) bool {
	t.Helper()
	var ok bool
	if err := x.target.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestExecuteSuccessChangesSchema(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	m := x.draft(t, "CREATE TABLE zz_mig_t1 (id int); INSERT INTO zz_mig_t1 VALUES (1);")
	got, err := x.e.Execute(ctx, 1, m.ID, x.uid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSuccess || got.Version == nil || *got.Version != 1 {
		t.Fatalf("hasil salah: %+v", got)
	}
	if got.ExecutedBy == nil || *got.ExecutedBy != x.uid || got.ExecutedByName != "alice" {
		t.Fatalf("pelaksana tidak tercatat: %+v", got)
	}
	if got.StartedAt == nil || got.FinishedAt == nil || got.ErrorMessage != "" {
		t.Fatalf("waktu/error salah: %+v", got)
	}
	var n int
	if err := x.target.QueryRow(ctx, "SELECT count(*) FROM zz_mig_t1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("data tidak masuk, n=%d", n)
	}
}

func TestExecuteErrorRollsBackEverything(t *testing.T) {
	x := setupExec(t)
	m := x.draft(t, "CREATE TABLE zz_mig_t2 (id int); INSERT INTO tabel_tidak_ada VALUES (1);")
	got, err := x.e.Execute(context.Background(), 1, m.ID, x.uid)
	if err != nil {
		t.Fatalf("migration gagal bukan error Go: %v", err)
	}
	if got.Status != StatusFailed || got.ErrorMessage == "" || got.Version == nil || *got.Version != 1 {
		t.Fatalf("hasil salah: %+v", got)
	}
	if x.tableExists(t, "zz_mig_t2") {
		t.Fatal("statement pertama tidak ikut di-rollback")
	}
}

func TestVersionsStayLinear(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	sqls := []string{"CREATE TABLE zz_mig_t1 (id int);", "SELECT salah_sintaks FROM;", "CREATE TABLE zz_mig_t3 (id int);"}
	for i, s := range sqls {
		got, err := x.e.Execute(ctx, 1, x.draft(t, s).ID, x.uid)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version == nil || *got.Version != i+1 {
			t.Fatalf("migration ke-%d mendapat versi %v", i+1, got.Version)
		}
	}
	list, _ := x.repo.List(ctx, 1)
	if len(list) != 3 || list[0].Status != StatusSuccess || list[1].Status != StatusFailed {
		t.Fatalf("riwayat salah: %+v", list)
	}
}

func TestExecuteTwiceRejected(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	m := x.draft(t, "SELECT 1;")
	if _, err := x.e.Execute(ctx, 1, m.ID, x.uid); err != nil {
		t.Fatal(err)
	}
	if _, err := x.e.Execute(ctx, 1, m.ID, x.uid); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("eksekusi ulang harus ErrNotDraft, got %v", err)
	}
	if _, err := x.e.Execute(ctx, 2, m.ID, x.uid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DB salah harus ErrNotFound, got %v", err)
	}
}

func TestParallelExecutionRejected(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	slow := x.draft(t, "SELECT pg_sleep(2);")
	fast := x.draft(t, "SELECT 1;")
	done := make(chan error, 1)
	go func() {
		_, err := x.e.Execute(ctx, 1, slow.ID, x.uid)
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got, err := x.repo.Get(ctx, 1, slow.ID); err == nil && got.Status == StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("migration pertama tidak pernah berstatus running")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := x.e.Execute(ctx, 1, fast.ID, x.uid); !errors.Is(err, ErrBusy) {
		t.Fatalf("eksekusi paralel harus ErrBusy, got %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, _ := x.repo.Get(ctx, 1, fast.ID)
	if after.Status != StatusDraft || after.Version != nil {
		t.Fatalf("migration yang ditolak harus tetap draft: %+v", after)
	}
	got, err := x.e.Execute(ctx, 1, fast.ID, x.uid)
	if err != nil || got.Status != StatusSuccess || *got.Version != 2 {
		t.Fatalf("setelah lock lepas harus bisa jalan: %v %+v", err, got)
	}
}

func TestStatementTimeout(t *testing.T) {
	x := setupExec(t)
	x.e.Timeout = 500 * time.Millisecond
	m := x.draft(t, "SELECT pg_sleep(5);")
	start := time.Now()
	got, err := x.e.Execute(context.Background(), 1, m.ID, x.uid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || got.ErrorMessage == "" {
		t.Fatalf("timeout harus berstatus failed: %+v", got)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout tidak memotong eksekusi")
	}
}

func TestTargetUnavailableKeepsDraft(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	bad, err := NewStaticTargets(ctx, 1, "mati", "postgres://app:app@127.0.0.1:1/x?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bad.Close)
	e := NewExecutor(x.db, x.repo, bad, 0)
	m := x.draft(t, "SELECT 1;")
	if _, err := e.Execute(ctx, 1, m.ID, x.uid); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("want ErrTargetUnavailable, got %v", err)
	}
	after, _ := x.repo.Get(ctx, 1, m.ID)
	if after.Status != StatusDraft || after.Version != nil {
		t.Fatalf("target mati tidak boleh memakai versi: %+v", after)
	}
}

func TestRecoverStuck(t *testing.T) {
	x := setupExec(t)
	var running int
	ctx := context.Background()
	_, err := x.db.Exec(ctx, "INSERT INTO migrations (db_id, version, title, sql_text, status, created_by) VALUES (1, 1, 't', 's', 'running', $1)", x.uid)
	if err != nil {
		t.Fatal(err)
	}
	n, err := x.e.RecoverStuck(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RecoverStuck: %d %v", n, err)
	}
	if err := x.db.QueryRow(ctx, "SELECT count(*) FROM migrations WHERE status = 'running'").Scan(&running); err != nil {
		t.Fatal(err)
	}
	if running != 0 {
		t.Fatalf("masih ada %d migration berstatus running", running)
	}
	list, _ := x.repo.List(ctx, 1)
	if len(list) != 1 || list[0].Status != StatusFailed {
		t.Fatalf("running harus menjadi failed: %+v", list)
	}
}
