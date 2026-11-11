package exec

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

// Bukti untuk README: jalankan dengan M2_BIGROWS=2000000 (naikkan sampai UPDATE tanpa batch > 1 detik).
//
//	M2_BIGROWS=2000000 make test-integration PKG=./internal/migration/exec RUN=TestBigDataTiming
func TestBigDataTiming(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("M2_BIGROWS"))
	if n == 0 {
		t.Skip("set M2_BIGROWS untuk mengukur")
	}
	r := newRig(t)
	r.h.StmtTimeout = 10 * time.Minute
	_, err := r.pool.Exec(r.ctx, `CREATE TABLE zz_big(id bigint PRIMARY KEY, x int NOT NULL DEFAULT 0, pad text)`)
	r.must(err)
	_, err = r.pool.Exec(r.ctx, `INSERT INTO zz_big(id, pad) SELECT g, md5(g::text) FROM generate_series(1,$1) g`, n)
	r.must(err)
	_, _ = r.pool.Exec(r.ctx, `ANALYZE zz_big`)

	// 1) tanpa batching, lewat queue + worker
	id := r.ready(r.mig("update penuh", `UPDATE zz_big SET x=1, pad=pad||'a'`, `UPDATE zz_big SET x=0`))
	t0 := time.Now()
	r.run(id)
	plain := time.Since(t0)
	if r.ex(id).Status != workflow.StatusSuccess {
		t.Fatalf("%+v", r.ex(id))
	}
	_, _ = r.pool.Exec(r.ctx, `UPDATE zz_big SET x=0`)

	// 2) dataset sama, mode batch
	up := `UPDATE zz_big SET x=2 WHERE id IN (SELECT id FROM zz_big WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id`
	b := r.batchMig("update batch", up, `UPDATE zz_big SET x=0 WHERE id > $1 AND id <= $2`, 50000)
	id2 := r.ready(b)
	t0 = time.Now()
	r.run(id2)
	batch := time.Since(t0)
	e := r.ex(id2)
	if e.Status != workflow.StatusSuccess || e.RowsCommitted != int64(n) {
		t.Fatalf("%+v", e)
	}
	t.Logf("BUKTI: rows=%d | tanpa batch=%s | batch(50000)=%s (%d batch, %d row)", n, plain, batch, e.BatchesCommitted, e.RowsCommitted)
}
