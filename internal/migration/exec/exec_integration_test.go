package exec

import (
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

func TestNormalSuccess(t *testing.T) {
	r := newRig(t)
	up := "CREATE TABLE zz_n(id int); INSERT INTO zz_n VALUES (1),(2)"
	id := r.ready(r.mig("buat zz_n", up, "DROP TABLE zz_n"))
	r.run(id)
	e := r.ex(id)
	if e.Status != workflow.StatusSuccess || e.Attempt != 1 || e.ExecutedSQL != up || e.FinishedAt == nil {
		t.Fatalf("hasil: %+v", e)
	}
	if r.count(`SELECT count(*) FROM zz_n`) != 2 {
		t.Fatal("data target harus 2 baris")
	}
	if r.hist() != 1 || r.locks() != 0 {
		t.Fatalf("history=%d locks=%d", r.hist(), r.locks())
	}
	if r.count(`SELECT count(*) FROM dbmp_meta.execution_marker WHERE execution_id=$1`, id) != 1 {
		t.Fatal("marker harus ada")
	}
}

func TestFailureRollsBackAndNoHistory(t *testing.T) {
	r := newRig(t)
	m := r.mig("gagal", "CREATE TABLE zz_f(id int); SELECT 1/0", "DROP TABLE zz_f")
	id := r.ready(m)
	r.run(id)
	e := r.ex(id)
	if e.Status != workflow.StatusFailed || !strings.Contains(e.ErrorMessage, "22012") {
		t.Fatalf("harus failed dengan SQLSTATE: %+v", e)
	}
	if r.tableExists("zz_f") {
		t.Fatal("transaksi harus di-rollback seluruhnya")
	}
	if r.hist() != 0 || r.locks() != 0 {
		t.Fatalf("tidak boleh ada history/lock: %d %d", r.hist(), r.locks())
	}
	// retry dicatat sebagai attempt baru
	_, err := r.svc.Enqueue(r.ctx, r.uid, r.db, id)
	r.must(err)
	r.run(id)
	if e := r.ex(id); e.Attempt != 2 || e.Status != workflow.StatusFailed {
		t.Fatalf("attempt harus 2: %+v", e)
	}
}

func TestDuplicateMessageNoDoubleCommit(t *testing.T) {
	r := newRig(t)
	id := r.ready(r.mig("dup", "CREATE TABLE zz_n(id int)", "DROP TABLE zz_n"))
	par(4, func(int) { _ = r.h.Handle(r.ctx, id) })
	if r.hist() != 1 {
		t.Fatalf("history harus 1, got %d", r.hist())
	}
	if e := r.ex(id); e.Status != workflow.StatusSuccess || e.Attempt != 1 {
		t.Fatalf("%+v", e)
	}
	r.run(id) // pesan terlambat
	if r.hist() != 1 {
		t.Fatal("pesan duplikat tidak boleh menggandakan")
	}
}

func TestOrderEnforcedAtExecution(t *testing.T) {
	r := newRig(t)
	a := r.mig("A", "CREATE TABLE zz_p(id int)", "DROP TABLE zz_p")
	b := r.mig("B", "ALTER TABLE zz_p ADD COLUMN c int", "ALTER TABLE zz_p DROP COLUMN c")
	ea, err := r.svc.Submit(r.ctx, r.uid, r.db, a)
	r.must(err)
	eb, err := r.svc.Submit(r.ctx, r.uid, r.db, b)
	r.must(err)
	r.approveQueue(eb.ID) // B disetujui & diantrekan lebih dulu
	r.run(eb.ID)
	if e := r.ex(eb.ID); e.Status != workflow.StatusFailed || !strings.Contains(e.ErrorMessage, "berikutnya") {
		t.Fatalf("B harus ditolak karena A belum jalan: %+v", e)
	}
	r.approveQueue(ea.ID)
	r.run(ea.ID)
	if r.ex(ea.ID).Status != workflow.StatusSuccess {
		t.Fatal("A harus sukses")
	}
	_, err = r.svc.Enqueue(r.ctx, r.uid, r.db, eb.ID) // retry B
	r.must(err)
	r.run(eb.ID)
	if r.ex(eb.ID).Status != workflow.StatusSuccess || r.hist() != 2 {
		t.Fatalf("B harus sukses setelah A: %+v", r.ex(eb.ID))
	}
}

func TestRollbackForwardAndHideRollback(t *testing.T) {
	r := newRig(t)
	a := r.mig("A", "CREATE TABLE zz_r(id int)", "DROP TABLE zz_r")
	e1 := r.ready(a)
	r.run(e1)
	if !r.tableExists("zz_r") {
		t.Fatal("up harus membuat tabel")
	}
	e3 := r.readyRollback(a)
	r.run(e3)
	if r.tableExists("zz_r") || r.ex(e3).Status != workflow.StatusSuccess {
		t.Fatal("rollback harus menghapus tabel")
	}
	var reverted int64
	r.must(r.pool.QueryRow(r.ctx, `SELECT reverted_execution_id FROM platform_migration_history WHERE execution_id=$1`, e3).Scan(&reverted))
	if reverted != e1 {
		t.Fatalf("rollback harus merujuk execution %d, got %d", e1, reverted)
	}
	hs, err := r.svc.History(r.ctx, r.uid, r.db, true)
	r.must(err)
	if len(hs) != 0 {
		t.Fatalf("hide rollback harus menyembunyikan pasangan: %d", len(hs))
	}
	// forward: kind otomatis forward, memakai SQL up revisi yang sama
	e4 := r.ready(a)
	if r.ex(e4).Kind != workflow.KindForward {
		t.Fatalf("harus forward: %s", r.ex(e4).Kind)
	}
	r.run(e4)
	if !r.tableExists("zz_r") {
		t.Fatal("forward harus membuat tabel lagi")
	}
	hs, _ = r.svc.History(r.ctx, r.uid, r.db, true)
	if len(hs) != 1 || hs[0].ExecutionID != e4 {
		t.Fatalf("hanya forward yang tampil: %+v", hs)
	}
	hs, _ = r.svc.History(r.ctx, r.uid, r.db, false)
	if len(hs) != 3 {
		t.Fatalf("riwayat lengkap harus 3, got %d", len(hs))
	}
}

func TestRollbackOnlyLastActiveAndIrreversible(t *testing.T) {
	r := newRig(t)
	a := r.mig("A", "CREATE TABLE zz_p(id int)", "DROP TABLE zz_p")
	b := r.mig("B", "CREATE TABLE zz_q(id int)", "DROP TABLE zz_q")
	r.run(r.ready(a))
	r.run(r.ready(b))
	if _, err := r.svc.Rollback(r.ctx, r.uid, r.db, a); err == nil || !strings.Contains(err.Error(), "terakhir") {
		t.Fatalf("rollback A sebelum B harus ditolak: %v", err)
	}
	// migration irreversible
	m, err := r.svc.Create(r.ctx, r.uid, r.db, workflow.CreateRequest{Title: "I", SQLUp: "SELECT 1",
		IsIrreversible: true, IrreversibleReason: "hapus data"})
	r.must(err)
	r.run(r.ready(m.ID))
	if _, err := r.svc.Rollback(r.ctx, r.uid, r.db, m.ID); err == nil || !strings.Contains(err.Error(), "irreversible") {
		t.Fatalf("irreversible harus ditolak: %v", err)
	}
}

func TestFailedRollbackKeepsPositionAndHidesNothing(t *testing.T) {
	r := newRig(t)
	a := r.mig("A", "CREATE TABLE zz_r(id int)", "DROP TABLE zz_nonexistent")
	r.run(r.ready(a))
	e := r.readyRollback(a)
	r.run(e)
	if r.ex(e).Status != workflow.StatusFailed {
		t.Fatalf("rollback harus failed: %+v", r.ex(e))
	}
	if !r.tableExists("zz_r") {
		t.Fatal("rollback gagal tidak boleh mengubah target")
	}
	items, _ := r.h.Repo.ItemsFor(r.ctx, r.db)
	if len(items) != 1 || !items[0].Active {
		t.Fatalf("posisi aktif harus tetap: %+v", items)
	}
	hs, _ := r.svc.History(r.ctx, r.uid, r.db, true)
	if len(hs) != 1 {
		t.Fatalf("tidak ada yang disembunyikan: %d", len(hs))
	}
}

func TestRevalidationApprovalAndAccess(t *testing.T) {
	r := newRig(t)
	id := r.ready(r.mig("A", "CREATE TABLE zz_p(id int)", "DROP TABLE zz_p"))
	r.apr.Revoke(id)
	r.run(id)
	if e := r.ex(id); e.Status != workflow.StatusFailed || !strings.Contains(e.ErrorMessage, "approval") {
		t.Fatalf("approval dicabut harus gagal: %+v", e)
	}
	if r.tableExists("zz_p") {
		t.Fatal("tidak boleh dieksekusi")
	}
	r.apr.Approve(id)
	r.acc.Grant(r.uid, r.db, 1) // akses edit dicabut
	_, err := r.svc.Enqueue(r.ctx, r.uid, r.db, id)
	if err == nil {
		t.Fatal("tanpa edit harus ditolak di API")
	}
	r.acc.Grant(r.uid, r.db, 2)
	_, err = r.svc.Enqueue(r.ctx, r.uid, r.db, id)
	r.must(err)
	r.acc.Grant(r.uid, r.db, 1) // dicabut setelah masuk antrean
	r.run(id)
	if e := r.ex(id); e.Status != workflow.StatusFailed || !strings.Contains(e.ErrorMessage, "akses") {
		t.Fatalf("akses dicabut harus gagal di worker: %+v", e)
	}
}

func TestRetryableWithDelayThenSuccess(t *testing.T) {
	r := newRig(t)
	_, err := r.pool.Exec(r.ctx, `CREATE TABLE zz_l(id int)`)
	r.must(err)
	id := r.ready(r.mig("alter", "ALTER TABLE zz_l ADD COLUMN c int", "ALTER TABLE zz_l DROP COLUMN c"))
	blocker, err := r.pool.Begin(r.ctx)
	r.must(err)
	_, err = blocker.Exec(r.ctx, `LOCK TABLE zz_l IN ACCESS EXCLUSIVE MODE`)
	r.must(err)
	before := r.q.Count()
	r.run(id) // lock_timeout 55P03 -> retryable
	e := r.ex(id)
	if e.Status != workflow.StatusQueued || e.Attempt != 1 {
		t.Fatalf("harus kembali queued: %+v", e)
	}
	if r.q.Count() != before+1 || r.q.Last().Delay <= 0 {
		t.Fatalf("retry harus dijadwalkan dengan jeda: %+v", r.q.Last())
	}
	if r.locks() != 0 {
		t.Fatal("lock dilepas saat retry penuh")
	}
	r.must(blocker.Rollback(r.ctx))
	r.run(id)
	if e := r.ex(id); e.Status != workflow.StatusSuccess || e.Attempt != 2 {
		t.Fatalf("attempt 2 harus sukses: %+v", e)
	}
}

func TestRetryLimited(t *testing.T) {
	r := newRig(t)
	_, err := r.pool.Exec(r.ctx, `CREATE TABLE zz_l(id int)`)
	r.must(err)
	id := r.ready(r.mig("alter", "ALTER TABLE zz_l ADD COLUMN c int", "ALTER TABLE zz_l DROP COLUMN c"))
	blocker, _ := r.pool.Begin(r.ctx)
	_, _ = blocker.Exec(r.ctx, `LOCK TABLE zz_l IN ACCESS EXCLUSIVE MODE`)
	defer blocker.Rollback(r.ctx)
	for i := 0; i < 3; i++ {
		r.run(id)
	}
	if e := r.ex(id); e.Status != workflow.StatusFailed || e.Attempt != 3 {
		t.Fatalf("setelah 3 attempt harus failed: %+v", e)
	}
	if r.hist() != 0 {
		t.Fatal("tidak ada history")
	}
}

func TestOneMigrationPerDatabase(t *testing.T) {
	r := newRig(t)
	a := r.mig("A", "SELECT pg_sleep(0)", "SELECT 1")
	b := r.mig("B", "SELECT 1", "SELECT 1")
	ea, _ := r.svc.Submit(r.ctx, r.uid, r.db, a)
	eb, _ := r.svc.Submit(r.ctx, r.uid, r.db, b)
	r.approveQueue(ea.ID)
	r.approveQueue(eb.ID)
	// A "sedang berjalan" (diklaim worker lain)
	_, _, ok, err := r.h.Repo.Claim(r.ctx, ea.ID)
	r.must(err)
	if !ok {
		t.Fatal("claim A")
	}
	before := r.q.Count()
	r.run(eb.ID) // B harus ditunda, bukan berjalan
	if e := r.ex(eb.ID); e.Status != workflow.StatusQueued || e.Attempt != 0 {
		t.Fatalf("B harus tetap queued: %+v", e)
	}
	if r.q.Count() != before+1 || r.q.Last().ExecID != eb.ID || r.q.Last().Delay <= 0 {
		t.Fatalf("B harus dijadwalkan ulang: %+v", r.q.Last())
	}
}

func TestLocksAcquireHeartbeatRelease(t *testing.T) {
	r := newRig(t)
	m1 := r.mig("A", "SELECT 1", "SELECT 1")
	m2 := r.mig("B", "SELECT 1", "SELECT 1")
	e1, _ := r.svc.Submit(r.ctx, r.uid, r.db, m1)
	e2, _ := r.svc.Submit(r.ctx, r.uid, r.db, m2)
	l := r.h.locks()
	ok, err := l.Acquire(r.ctx, r.db, e1.ID, "w1", 5e9)
	r.must(err)
	if !ok {
		t.Fatal("acquire pertama harus sukses")
	}
	if ok, _ := l.Acquire(r.ctx, r.db, e2.ID, "w2", 5e9); ok {
		t.Fatal("eksekusi lain tidak boleh merebut lock")
	}
	if ok, _ := l.Heartbeat(r.ctx, r.db, e1.ID, "w1", 5e9); !ok {
		t.Fatal("heartbeat pemilik harus sukses")
	}
	if ok, _ := l.Heartbeat(r.ctx, r.db, e1.ID, "w-lain", 5e9); ok {
		t.Fatal("heartbeat worker lain harus gagal")
	}
	r.must(l.Release(r.ctx, r.db, e1.ID))
	if ok, _ := l.Acquire(r.ctx, r.db, e2.ID, "w2", 5e9); !ok {
		t.Fatal("setelah release, eksekusi lain boleh")
	}
}

// Worker mati setelah commit target tetapi sebelum mencatat hasil: marker membuktikan commit.
func TestCrashAfterCommitUsesMarker(t *testing.T) {
	r := newRig(t)
	id := r.ready(r.mig("crash", "CREATE TABLE zz_u(id int)", "DROP TABLE zz_u"))
	// simulasi: SQL + marker sudah commit di target, platform masih 'running' tanpa lock
	r.must(ensureMeta(r.ctx, r.pool))
	_, err := r.pool.Exec(r.ctx, `CREATE TABLE zz_u(id int)`)
	r.must(err)
	_, err = r.pool.Exec(r.ctx, `INSERT INTO dbmp_meta.execution_marker(execution_id, kind) VALUES ($1,'up')`, id)
	r.must(err)
	_, err = r.pool.Exec(r.ctx, `UPDATE platform_migration_executions SET status='running', attempt=1,
		started_at=now(), updated_at=now()-interval '1 minute' WHERE id=$1`, id)
	r.must(err)
	ids, err := r.h.RecoverStale(r.ctx)
	r.must(err)
	if len(ids) != 1 || ids[0] != id || r.ex(id).Status != workflow.StatusUncertain {
		t.Fatalf("recovery harus menandai uncertain: %v %+v", ids, r.ex(id))
	}
	if r.locks() != 1 {
		t.Fatal("lock harus ditahan selama uncertain")
	}
	r.run(id) // SQL TIDAK boleh dijalankan ulang (CREATE TABLE akan gagal)
	if e := r.ex(id); e.Status != workflow.StatusSuccess {
		t.Fatalf("marker ada → harus success tanpa rerun: %+v", e)
	}
	if r.hist() != 1 || r.locks() != 0 {
		t.Fatalf("history=%d locks=%d", r.hist(), r.locks())
	}
}

func TestCrashBeforeCommitReruns(t *testing.T) {
	r := newRig(t)
	id := r.ready(r.mig("crash2", "CREATE TABLE zz_u(id int)", "DROP TABLE zz_u"))
	_, err := r.pool.Exec(r.ctx, `UPDATE platform_migration_executions SET status='running', attempt=1,
		started_at=now(), updated_at=now()-interval '1 minute' WHERE id=$1`, id)
	r.must(err)
	_, err = r.h.RecoverStale(r.ctx)
	r.must(err)
	r.run(id)
	if e := r.ex(id); e.Status != workflow.StatusSuccess || !r.tableExists("zz_u") {
		t.Fatalf("tanpa marker harus dijalankan: %+v", e)
	}
}

func TestUncertainHoldsNextMigration(t *testing.T) {
	r := newRig(t)
	a := r.mig("A", "SELECT 1", "SELECT 1")
	b := r.mig("B", "SELECT 1", "SELECT 1")
	ea := r.ready(a)
	eb := r.ready(b)
	_, err := r.pool.Exec(r.ctx, `UPDATE platform_migration_executions SET status='uncertain' WHERE id=$1`, ea)
	r.must(err)
	r.run(eb)
	if e := r.ex(eb); e.Status != workflow.StatusQueued || e.Attempt != 0 {
		t.Fatalf("B harus ditahan selama A uncertain: %+v", e)
	}
}
