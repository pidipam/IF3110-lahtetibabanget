package explorer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

var ErrTableNotFound = errors.New("tabel tidak ditemukan")

const statementTimeoutMS = "5000"

// diisi oleh *pgxpool.Pool.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

type DataPage struct {
	Table      *Table
	Columns    []string
	Rows       [][]Cell
	Pagination Pagination
	Ordering   Ordering
}

func FetchPage(ctx context.Context, db TxBeginner, cat *Catalog, schema, table string, page, size int) (*DataPage, error) {
	t, ok := cat.Table(schema, table)
	if !ok {
		return nil, ErrTableNotFound
	}
	dp, err := fetchPage(ctx, db, t, page, size)
	if err != nil {
		return nil, dbconn.Wrap(err)
	}
	return dp, nil
}

func fetchPage(ctx context.Context, db TxBeginner, t *Table, page, size int) (*DataPage, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if _, err := tx.Exec(ctx, "SELECT set_config('statement_timeout', $1, true)", statementTimeoutMS); err != nil {
		return nil, err
	}

	var total int64
	if err := tx.QueryRow(ctx, BuildCount(t)).Scan(&total); err != nil {
		return nil, err
	}
	pg := Paginate(total, page, size)
	ord := ChooseOrdering(t)
	dp := &DataPage{Table: t, Pagination: pg, Ordering: ord}
	for _, c := range t.Columns {
		dp.Columns = append(dp.Columns, c.Name)
	}

	sql, ok := BuildSelect(t, ord)
	if !ok || total == 0 {
		return dp, nil
	}
	rows, err := tx.Query(ctx, sql, pg.PageSize, pg.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	n := len(t.Columns)
	for rows.Next() {
		vals := make([]*string, n)
		dest := make([]any, n)
		for i := range vals {
			dest[i] = &vals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan baris: %w", err)
		}
		row := make([]Cell, n)
		for i, v := range vals {
			row[i] = NewCell(v)
		}
		dp.Rows = append(dp.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dp, nil
}
