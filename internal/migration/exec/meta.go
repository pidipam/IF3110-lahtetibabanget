package exec

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Skema bantu di database TARGET. Marker ditulis dalam transaksi yang sama dengan
// SQL migration, sehingga keberadaannya membuktikan commit.
const metaDDL = `
CREATE SCHEMA IF NOT EXISTS dbmp_meta;
CREATE TABLE IF NOT EXISTS dbmp_meta.execution_marker (
	execution_id BIGINT PRIMARY KEY,
	kind TEXT NOT NULL,
	committed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS dbmp_meta.batch_checkpoint (
	execution_id BIGINT NOT NULL,
	batch_no INT NOT NULL,
	cursor_from BIGINT NOT NULL,
	cursor_to BIGINT NOT NULL,
	row_count INT NOT NULL,
	committed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	reverted_at TIMESTAMPTZ,
	PRIMARY KEY (execution_id, batch_no)
);`

func ensureMeta(ctx context.Context, p *pgxpool.Pool) error {
	_, err := p.Exec(ctx, metaDDL, pgx.QueryExecModeSimpleProtocol)
	return err
}

func hasMarker(ctx context.Context, p *pgxpool.Pool, execID int64) (bool, error) {
	var one int
	err := p.QueryRow(ctx, `SELECT 1 FROM dbmp_meta.execution_marker WHERE execution_id=$1`, execID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// lastCheckpoint: nomor batch terakhir yang aktif (belum di-revert), cursor, dan total row.
func lastCheckpoint(ctx context.Context, p *pgxpool.Pool, execID int64) (batchNo int, cursor int64, rows int64, err error) {
	err = p.QueryRow(ctx, `SELECT COALESCE(MAX(batch_no),0), COALESCE(MAX(cursor_to),0), COALESCE(SUM(row_count),0)
		FROM dbmp_meta.batch_checkpoint WHERE execution_id=$1 AND reverted_at IS NULL`, execID).Scan(&batchNo, &cursor, &rows)
	return
}

type checkpoint struct {
	No       int
	From, To int64
}

// pendingReverts: checkpoint yang belum dikembalikan, urutan batch terbalik.
func pendingReverts(ctx context.Context, p *pgxpool.Pool, execID int64) ([]checkpoint, error) {
	rows, err := p.Query(ctx, `SELECT batch_no, cursor_from, cursor_to FROM dbmp_meta.batch_checkpoint
		WHERE execution_id=$1 AND reverted_at IS NULL ORDER BY batch_no DESC`, execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checkpoint
	for rows.Next() {
		var c checkpoint
		if err := rows.Scan(&c.No, &c.From, &c.To); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
