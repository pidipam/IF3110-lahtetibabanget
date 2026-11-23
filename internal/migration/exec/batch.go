package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

func pgErr(err error) string {
	var pe *pgconn.PgError
	if asPg(err, &pe) {
		return pe.Message
	}
	return ""
}

// runBatches: tiap batch = satu transaksi (SQL + checkpoint). Melanjutkan dari checkpoint terakhir.
// Kolom batch diasumsikan numerik non-negatif; cursor awal 0.
func (h *Handler) runBatches(ctx context.Context, e workflow.Execution, rev workflow.Revision, pool *pgxpool.Pool) outcome {
	if err := h.Repo.SetExecutedSQL(ctx, e.ID, rev.SQLUp); err != nil {
		return outcome{class: Retryable, err: err}
	}
	batchNo, cursor, total, err := lastCheckpoint(ctx, pool, e.ID)
	if err != nil {
		return outcome{class: Retryable, err: fmt.Errorf("membaca checkpoint: %w", err)}
	}
	if batchNo > 0 {
		h.log(ctx, e, "batch.resume", "from_batch", batchNo, "cursor", cursor)
	}
	for {
		if ctx.Err() != nil {
			return outcome{class: Retryable, err: ctx.Err(), committed: batchNo}
		}
		n, max, err := h.oneBatch(ctx, pool, e, rev, batchNo+1, cursor)
		if err != nil {
			cl := Classify(err.err, err.stage)
			if err.perm {
				cl = Permanent
			}
			return outcome{class: cl, err: err.err, committed: batchNo}
		}
		if n == 0 {
			return outcome{class: Success, sql: rev.SQLUp, committed: batchNo}
		}
		batchNo++
		cursor = max
		total += int64(n)
		_ = h.Repo.SetProgress(ctx, e.ID, batchNo, total)
		h.log(ctx, e, "batch.commit", "batch", batchNo, "rows", n, "cursor", cursor, "total_rows", total)
	}
}

type batchErr struct {
	err   error
	stage Stage
	perm  bool
}

func (h *Handler) oneBatch(ctx context.Context, pool *pgxpool.Pool, e workflow.Execution, rev workflow.Revision, no int, cursor int64) (int, int64, *batchErr) {
	tx, err := h.beginTx(ctx, pool)
	if err != nil {
		return 0, 0, &batchErr{err: err}
	}
	rb := func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }
	t0 := time.Now()
	rows, err := tx.Query(ctx, rev.SQLUp, cursor, int64(rev.BatchSize))
	if err != nil {
		rb()
		return 0, 0, &batchErr{err: fmt.Errorf("batch %d gagal (SQLSTATE %s): %s", no, SQLState(err), errMsg(err))}
	}
	n, max := 0, cursor
	for rows.Next() { // hanya nilai kolom batch yang dibaca: memori terbatas
		var v int64
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			rb()
			return 0, 0, &batchErr{err: fmt.Errorf("kolom batch harus numerik: %w", err), perm: true}
		}
		n++
		if v > max {
			max = v
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		rb()
		return 0, 0, &batchErr{err: fmt.Errorf("batch %d gagal (SQLSTATE %s): %s", no, SQLState(err), errMsg(err))}
	}
	if n == 0 {
		rb()
		return 0, 0, nil
	}
	if max <= cursor {
		rb()
		return 0, 0, &batchErr{err: fmt.Errorf("kolom batch tidak bertambah; SQL harus memproses id > $1"), perm: true}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dbmp_meta.batch_checkpoint (execution_id, batch_no, cursor_from, cursor_to, row_count)
		VALUES ($1,$2,$3,$4,$5)`, e.ID, no, cursor, max, n); err != nil {
		rb()
		return 0, 0, &batchErr{err: err}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, &batchErr{err: fmt.Errorf("hasil commit batch %d tidak pasti: %w", no, err), stage: StageCommit}
	}
	h.log(ctx, e, "batch.sql", "batch", no, "duration_ms", time.Since(t0).Milliseconds())
	return n, max, nil
}

// revertRanges menjalankan SQL down per rentang, urutan batch terbalik, tiap rentang satu transaksi.
// Dipakai untuk (a) kompensasi eksekusi partial dan (b) rollback migration batch.
func (h *Handler) revertRanges(ctx context.Context, e workflow.Execution, rev workflow.Revision, pool *pgxpool.Pool, src int64) outcome {
	if err := h.Repo.SetExecutedSQL(ctx, e.ID, rev.SQLDown); err != nil {
		return outcome{class: Retryable, err: err, reverting: true}
	}
	cps, err := pendingReverts(ctx, pool, src)
	if err != nil {
		return outcome{class: Retryable, err: err, reverting: true}
	}
	done := 0
	for _, c := range cps {
		if ctx.Err() != nil {
			return outcome{class: Retryable, err: ctx.Err(), reverting: true, committed: done}
		}
		tx, err := h.beginTx(ctx, pool)
		if err != nil {
			return outcome{class: Classify(err, StageRun), err: err, reverting: true, committed: done}
		}
		if _, err := tx.Exec(ctx, rev.SQLDown, c.From, c.To); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return outcome{class: Classify(err, StageRun), reverting: true, committed: done,
				err: fmt.Errorf("pembatalan batch %d gagal (SQLSTATE %s): %s", c.No, SQLState(err), errMsg(err))}
		}
		if _, err := tx.Exec(ctx, `UPDATE dbmp_meta.batch_checkpoint SET reverted_at=now()
			WHERE execution_id=$1 AND batch_no=$2`, src, c.No); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return outcome{class: Classify(err, StageRun), err: err, reverting: true, committed: done}
		}
		if err := tx.Commit(ctx); err != nil {
			return outcome{class: Classify(err, StageCommit), err: err, reverting: true, committed: done}
		}
		done++
		h.log(ctx, e, "batch.reverted", "batch", c.No, "from", c.From, "to", c.To)
	}
	return outcome{class: Success, sql: rev.SQLDown, reverting: true, committed: done}
}
