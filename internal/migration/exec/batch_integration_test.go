package exec

import (
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

const (
	batchUp   = `UPDATE zz_big SET x=1 WHERE id IN (SELECT id FROM zz_big WHERE id > $1 AND zz_chk($1, 2000) ORDER BY id LIMIT $2) RETURNING id`
	batchDown = `UPDATE zz_big SET x=0 WHERE id > $1 AND id <= $2 AND zz_chk($1, 0)`
)

func (r *rig) bigTable(rows int) {
	r.t.Helper()
	for _, q := range []string{
		`CREATE TABLE zz_ctl(v int)`, `INSERT INTO zz_ctl VALUES (0)`,
		`CREATE TABLE zz_big(id bigint PRIMARY KEY, x int NOT NULL DEFAULT 0)`,
		`CREATE FUNCTION zz_chk(c bigint, bad bigint) RETURNS bool LANGUAGE plpgsql STABLE AS $$
			BEGIN IF (SELECT v FROM zz_ctl LIMIT 1) = 1 AND c = bad THEN RAISE EXCEPTION 'gagal buatan di cursor %', c; END IF; RETURN true; END $$`,
	} {
		_, err := r.pool.Exec(r.ctx, q)
		r.must(err)
	}
	_, err := r.pool.Exec(r.ctx, `INSERT INTO zz_big(id) SELECT g FROM generate_series(1,$1) g`, rows)
	r.must(err)
}

func (r *rig) setCtl(v int) {
	_, err := r.pool.Exec(r.ctx, `UPDATE zz_ctl SET v=$1`, v)
	r.must(err)
}

func TestBatchSuccess(t *testing.T) {
	r := newRig(t)
	r.bigTable(5000)
	id := r.ready(r.batchMig("batch", batchUp, batchDown, 1000))
	r.run(id)
	e := r.ex(id)
	if e.Status != workflow.StatusSuccess || e.BatchesCommitted != 5 || e.RowsCommitted != 5000 {
		t.Fatalf("hasil: %+v", e)
	}
	if r.count(`SELECT count(*) FROM zz_big WHERE x=1`) != 5000 {
		t.Fatal("semua baris harus terupdate")
	}
	if r.count(`SELECT count(*) FROM dbmp_meta.batch_checkpoint WHERE execution_id=$1`, id) != 5 {
		t.Fatal("harus ada 5 checkpoint")
	}
	if r.hist() != 1 || r.locks() != 0 {
		t.Fatal("history 1, lock lepas")
	}
}

func TestBatchPartialThenResumeWithoutRework(t *testing.T) {
	r := newRig(t)
	r.bigTable(5000)
	r.setCtl(1) // batch ke-3 (cursor 2000) akan gagal
	id := r.ready(r.batchMig("batch", batchUp, batchDown, 1000))
	r.run(id)
	e := r.ex(id)
	if e.Status != workflow.StatusPartial || e.BatchesCommitted != 2 || e.RowsCommitted != 2000 {
		t.Fatalf("harus partial 2 batch: %+v", e)
	}
	if r.count(`SELECT count(*) FROM zz_big WHERE x=1`) != 2000 {
		t.Fatal("batch 1-2 tetap committed, batch 3 di-rollback")
	}
	if r.hist() != 0 || r.locks() != 1 {
		t.Fatalf("partial: tanpa history, lock ditahan (hist=%d locks=%d)", r.hist(), r.locks())
	}
	// migration berikutnya pada database yang sama harus menunggu
	other := r.ready(r.mig("B", "SELECT 1", "SELECT 1"))
	r.run(other)
	if o := r.ex(other); o.Status != workflow.StatusQueued || o.Attempt != 0 {
		t.Fatalf("B harus menunggu: %+v", o)
	}
	// perbaiki kondisi lalu lanjutkan dari checkpoint
	r.setCtl(0)
	_, err := r.svc.Enqueue(r.ctx, r.uid, r.db, id) // resume
	r.must(err)
	r.run(id)
	e = r.ex(id)
	if e.Status != workflow.StatusSuccess || e.BatchesCommitted != 5 || e.RowsCommitted != 5000 {
		t.Fatalf("resume harus selesai: %+v", e)
	}
	if r.count(`SELECT count(*) FROM dbmp_meta.batch_checkpoint WHERE execution_id=$1`, id) != 5 ||
		r.count(`SELECT sum(row_count) FROM dbmp_meta.batch_checkpoint WHERE execution_id=$1`, id) != 5000 {
		t.Fatal("tidak boleh ada batch yang diulang")
	}
	if r.hist() != 1 || r.locks() != 0 {
		t.Fatalf("hist=%d locks=%d", r.hist(), r.locks())
	}
	r.run(other) // B sekarang boleh
	if r.ex(other).Status != workflow.StatusSuccess {
		t.Fatalf("B harus sukses setelah lock lepas: %+v", r.ex(other))
	}
}

func TestBatchRevertCompensationPartiallyReverted(t *testing.T) {
	r := newRig(t)
	r.bigTable(5000)
	r.setCtl(1)
	id := r.ready(r.batchMig("batch", batchUp, batchDown, 1000))
	r.run(id) // partial: batch 1-2 committed
	if r.ex(id).Status != workflow.StatusPartial {
		t.Fatalf("harus partial: %+v", r.ex(id))
	}
	_, err := r.svc.RevertBatches(r.ctx, r.uid, r.db, id)
	r.must(err)
	r.run(id) // revert (1000,2000] berhasil; (0,1000] gagal (ctl masih 1)
	e := r.ex(id)
	if e.Status != workflow.StatusPartiallyReverted {
		t.Fatalf("harus partially_reverted: %+v", e)
	}
	if r.count(`SELECT count(*) FROM zz_big WHERE x=1`) != 1000 {
		t.Fatal("hanya satu rentang yang sudah dikembalikan")
	}
	if r.locks() != 1 {
		t.Fatal("lock tetap ditahan")
	}
	r.setCtl(0)
	_, err = r.svc.RevertBatches(r.ctx, r.uid, r.db, id)
	r.must(err)
	r.run(id)
	e = r.ex(id)
	if e.Status != workflow.StatusCancelled {
		t.Fatalf("kompensasi selesai → cancelled: %+v", e)
	}
	if r.count(`SELECT count(*) FROM zz_big WHERE x=1`) != 0 || r.hist() != 0 || r.locks() != 0 {
		t.Fatal("semua kembali, tanpa history, lock lepas")
	}
	if r.count(`SELECT count(*) FROM dbmp_meta.batch_checkpoint WHERE execution_id=$1 AND reverted_at IS NULL`, id) != 0 {
		t.Fatal("semua checkpoint harus tertandai reverted")
	}
}

func TestBatchRollbackAfterSuccess(t *testing.T) {
	r := newRig(t)
	r.bigTable(3000)
	m := r.batchMig("batch", batchUp, batchDown, 1000)
	e1 := r.ready(m)
	r.run(e1)
	if r.ex(e1).Status != workflow.StatusSuccess {
		t.Fatalf("%+v", r.ex(e1))
	}
	e2 := r.readyRollback(m)
	r.run(e2)
	if r.ex(e2).Status != workflow.StatusSuccess {
		t.Fatalf("rollback batch: %+v", r.ex(e2))
	}
	if r.count(`SELECT count(*) FROM zz_big WHERE x=1`) != 0 {
		t.Fatal("rollback harus mengembalikan semua batch")
	}
	hs, _ := r.svc.History(r.ctx, r.uid, r.db, true)
	if len(hs) != 0 {
		t.Fatalf("up + rollback tersembunyi: %d", len(hs))
	}
}

func TestBatchBadColumnType(t *testing.T) {
	r := newRig(t)
	_, err := r.pool.Exec(r.ctx, `CREATE TABLE zz_big(id text, x int DEFAULT 0); INSERT INTO zz_big VALUES ('a',0)`)
	r.must(err)
	up := `UPDATE zz_big SET x=1 WHERE id IN (SELECT id FROM zz_big WHERE id::text > $1::text ORDER BY id LIMIT $2) RETURNING id`
	id := r.ready(r.batchMig("txt", up, "UPDATE zz_big SET x=0 WHERE $1=$1 AND $2=$2", 10))
	r.run(id)
	if e := r.ex(id); e.Status != workflow.StatusFailed {
		t.Fatalf("kolom non-numerik harus gagal permanen: %+v", e)
	}
}

func TestResumeAndRevertStillRequireApproval(t *testing.T) {
	r := newRig(t)
	r.bigTable(3000)
	r.setCtl(1)
	id := r.ready(r.batchMig("batch", batchUp, batchDown, 1000))
	r.run(id) // partial
	if r.ex(id).Status != workflow.StatusPartial {
		t.Fatalf("harus partial: %+v", r.ex(id))
	}
	r.apr.Revoke(id) // approval dicabut
	r.setCtl(0)
	_, err := r.svc.Enqueue(r.ctx, r.uid, r.db, id) // resume
	r.must(err)
	r.run(id)
	if e := r.ex(id); e.Status == workflow.StatusSuccess {
		t.Fatalf("resume tanpa approval tidak boleh sukses: %+v", e)
	}
}
