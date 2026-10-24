package explorer

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

// dipenuhi oleh *pgxpool.Pool.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const introspectTimeout = 10 * time.Second

const schemaFilter = `n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'`

const relkinds = `('r','p','v','m','f')`

const (
	qSchemas = `SELECT n.nspname FROM pg_namespace n WHERE ` + schemaFilter + ` ORDER BY n.nspname`

	qTables = `SELECT n.nspname, c.relname, c.relkind::text
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ` + relkinds + ` AND ` + schemaFilter + `
ORDER BY n.nspname, c.relname`

	qColumns = `SELECT n.nspname, c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
       a.attnum::int, NOT a.attnotnull, pg_get_expr(d.adbin, d.adrelid)
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attnum > 0 AND NOT a.attisdropped AND c.relkind IN ` + relkinds + ` AND ` + schemaFilter + `
ORDER BY n.nspname, c.relname, a.attnum`

	qConstraints = `SELECT n.nspname, c.relname, con.conname, con.contype::text, pg_get_constraintdef(con.oid),
       ARRAY(SELECT a.attname::text
             FROM unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
             JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
             ORDER BY k.ord)
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE con.contype IN ('p','u','f','c') AND c.relkind IN ` + relkinds + ` AND ` + schemaFilter + `
ORDER BY n.nspname, c.relname, con.conname`

	qIndexes = `SELECT n.nspname, t.relname, i.relname, pg_get_indexdef(ix.indexrelid), ix.indisunique, ix.indisprimary
FROM pg_index ix
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_class t ON t.oid = ix.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE t.relkind IN ` + relkinds + ` AND ` + schemaFilter + `
ORDER BY n.nspname, t.relname, i.relname`
)

func Introspect(ctx context.Context, q Querier) (*Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, introspectTimeout)
	defer cancel()
	cat, err := introspect(ctx, q)
	if err != nil {
		return nil, dbconn.Wrap(err)
	}
	return cat, nil
}

func introspect(ctx context.Context, q Querier) (*Catalog, error) {
	schemas, err := collect(ctx, q, qSchemas, func(r pgx.Rows) (string, error) {
		var s string
		return s, r.Scan(&s)
	})
	if err != nil {
		return nil, err
	}
	tables, err := collect(ctx, q, qTables, func(r pgx.Rows) (rawTable, error) {
		var t rawTable
		return t, r.Scan(&t.Schema, &t.Name, &t.Relkind)
	})
	if err != nil {
		return nil, err
	}
	cols, err := collect(ctx, q, qColumns, func(r pgx.Rows) (rawColumn, error) {
		var c rawColumn
		return c, r.Scan(&c.Schema, &c.Table, &c.Name, &c.Type, &c.Position, &c.Nullable, &c.Default)
	})
	if err != nil {
		return nil, err
	}
	cons, err := collect(ctx, q, qConstraints, func(r pgx.Rows) (rawConstraint, error) {
		var c rawConstraint
		return c, r.Scan(&c.Schema, &c.Table, &c.Name, &c.Contype, &c.Def, &c.Columns)
	})
	if err != nil {
		return nil, err
	}
	idxs, err := collect(ctx, q, qIndexes, func(r pgx.Rows) (rawIndex, error) {
		var i rawIndex
		return i, r.Scan(&i.Schema, &i.Table, &i.Name, &i.Def, &i.Unique, &i.Primary)
	})
	if err != nil {
		return nil, err
	}
	return assemble(schemas, tables, cols, cons, idxs), nil
}

func collect[T any](ctx context.Context, q Querier, sql string, scan func(pgx.Rows) (T, error)) ([]T, error) {
	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
