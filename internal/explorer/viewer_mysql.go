package explorer

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

func FetchPageSQL(ctx context.Context, db *sql.DB, cat *Catalog, schema, table string, page, size int) (*DataPage, error) {
	t, ok := cat.Table(schema, table)
	if !ok {
		return nil, ErrTableNotFound
	}
	dp, err := fetchPageSQL(ctx, db, t, page, size)
	if err != nil {
		return nil, dbconn.Wrap(err)
	}
	return dp, nil
}

func fetchPageSQL(ctx context.Context, db *sql.DB, t *Table, page, size int) (*DataPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second) // setara statement_timeout viewer
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var total int64
	if err := tx.QueryRowContext(ctx, BuildCountMySQL(t)).Scan(&total); err != nil {
		return nil, err
	}
	pg := Paginate(total, page, size)
	ord := ChooseOrderingMySQL(t)
	dp := &DataPage{Table: t, Pagination: pg, Ordering: ord}
	for _, c := range t.Columns {
		dp.Columns = append(dp.Columns, c.Name)
	}
	q, ok := BuildSelectMySQL(t, ord)
	if !ok || total == 0 {
		return dp, nil
	}
	rows, err := tx.QueryContext(ctx, q, pg.PageSize, pg.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	n := len(t.Columns)
	for rows.Next() {
		vals := make([]sql.NullString, n)
		dest := make([]any, n)
		for i := range vals {
			dest[i] = &vals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan baris: %w", err)
		}
		row := make([]Cell, n)
		for i, v := range vals {
			if v.Valid {
				s := v.String
				row[i] = NewCell(&s)
			} else {
				row[i] = NewCell(nil)
			}
		}
		dp.Rows = append(dp.Rows, row)
	}
	return dp, rows.Err()
}
