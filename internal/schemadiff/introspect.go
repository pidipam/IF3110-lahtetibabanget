package schemadiff

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
)

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const (
	qTables = `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind = 'r' AND NOT c.relispartition
  AND NOT EXISTS (SELECT 1 FROM pg_depend dep WHERE dep.classid = 'pg_class'::regclass AND dep.objid = c.oid AND dep.deptype = 'e')
ORDER BY c.relname`

	qColumns = `SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
       pg_get_expr(d.adbin, d.adrelid), a.attidentity::text, a.attgenerated::text
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE n.nspname = $1 AND c.relkind = 'r' AND NOT c.relispartition AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY c.relname, a.attnum`

	qConstraints = `SELECT c.relname, con.conname, con.contype::text, pg_get_constraintdef(con.oid)
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind = 'r' AND NOT c.relispartition AND con.contype IN ('p', 'u', 'f', 'c', 'x')
ORDER BY c.relname, con.conname`

	qIndexes = `SELECT t.relname, i.relname, ix.indisunique, substring(pg_get_indexdef(ix.indexrelid) from ' USING .*$')
FROM pg_index ix
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_class t ON t.oid = ix.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname = $1 AND t.relkind = 'r' AND NOT t.relispartition
  AND NOT EXISTS (SELECT 1 FROM pg_constraint con WHERE con.conindid = ix.indexrelid
                  AND con.conrelid = ix.indrelid AND con.contype IN ('p', 'u', 'x'))
ORDER BY t.relname, i.relname`

	qViews = `SELECT c.relname, pg_get_viewdef(c.oid, true) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind = 'v'
  AND NOT EXISTS (SELECT 1 FROM pg_depend dep WHERE dep.classid = 'pg_class'::regclass AND dep.objid = c.oid AND dep.deptype = 'e')
ORDER BY c.oid`

	qEnums = `SELECT t.typname, ARRAY(SELECT e.enumlabel::text FROM pg_enum e WHERE e.enumtypid = t.oid ORDER BY e.enumsortorder)
FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE n.nspname = $1 AND t.typtype = 'e'
  AND NOT EXISTS (SELECT 1 FROM pg_depend dep WHERE dep.classid = 'pg_type'::regclass AND dep.objid = t.oid AND dep.deptype = 'e')
ORDER BY t.oid`

	qSequences = `SELECT c.relname, COALESCE(o.relname, ''), COALESCE(o.attname, '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN LATERAL (
    SELECT t.relname::text, a.attname::text FROM pg_depend dep
    JOIN pg_class t ON t.oid = dep.refobjid
    JOIN pg_attribute a ON a.attrelid = dep.refobjid AND a.attnum = dep.refobjsubid
    WHERE dep.classid = 'pg_class'::regclass AND dep.objid = c.oid
      AND dep.refclassid = 'pg_class'::regclass AND dep.deptype = 'a'
    LIMIT 1) o ON true
WHERE n.nspname = $1 AND c.relkind = 'S'
  AND NOT EXISTS (SELECT 1 FROM pg_depend dep WHERE dep.classid = 'pg_class'::regclass AND dep.objid = c.oid AND dep.deptype IN ('i', 'e'))
ORDER BY c.relname`
)

func Introspect(ctx context.Context, q Querier, schema string) (*Schema, error) {
	s := &Schema{Name: schema}
	byName := map[string]*Table{}
	names, err := collect(ctx, q, qTables, schema, func(r pgx.Rows) (string, error) {
		var n string
		return n, r.Scan(&n)
	})
	if err != nil {
		return nil, err
	}
	s.Tables = make([]Table, len(names))
	for i, n := range names {
		s.Tables[i].Name = n
		byName[n] = &s.Tables[i]
	}

	type colRow struct {
		table string
		col   Column
	}
	cols, err := collect(ctx, q, qColumns, schema, func(r pgx.Rows) (colRow, error) {
		var c colRow
		var def *string
		err := r.Scan(&c.table, &c.col.Name, &c.col.Type, &c.col.NotNull, &def, &c.col.Identity, &c.col.Generated)
		if def != nil {
			if c.col.Generated != "" {
				c.col.Expression = *def
			} else {
				c.col.HasDefault, c.col.Default = true, *def
			}
		}
		return c, err
	})
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		if t := byName[c.table]; t != nil {
			t.Columns = append(t.Columns, c.col)
		}
	}

	type conRow struct {
		table string
		con   Constraint
	}
	cons, err := collect(ctx, q, qConstraints, schema, func(r pgx.Rows) (conRow, error) {
		var c conRow
		return c, r.Scan(&c.table, &c.con.Name, &c.con.Type, &c.con.Definition)
	})
	if err != nil {
		return nil, err
	}
	for _, c := range cons {
		if t := byName[c.table]; t != nil {
			t.Constraints = append(t.Constraints, c.con)
		}
	}

	type idxRow struct {
		table string
		idx   Index
	}
	idxs, err := collect(ctx, q, qIndexes, schema, func(r pgx.Rows) (idxRow, error) {
		var i idxRow
		var body *string
		err := r.Scan(&i.table, &i.idx.Name, &i.idx.Unique, &body)
		if body != nil {
			i.idx.Body = trimUsing(*body)
		}
		return i, err
	})
	if err != nil {
		return nil, err
	}
	for _, i := range idxs {
		if t := byName[i.table]; t != nil {
			t.Indexes = append(t.Indexes, i.idx)
		}
	}

	if s.Views, err = collect(ctx, q, qViews, schema, func(r pgx.Rows) (View, error) {
		var v View
		return v, r.Scan(&v.Name, &v.Definition)
	}); err != nil {
		return nil, err
	}
	if s.Enums, err = collect(ctx, q, qEnums, schema, func(r pgx.Rows) (Enum, error) {
		var e Enum
		return e, r.Scan(&e.Name, &e.Labels)
	}); err != nil {
		return nil, err
	}
	if s.Sequences, err = collect(ctx, q, qSequences, schema, func(r pgx.Rows) (Sequence, error) {
		var sq Sequence
		return sq, r.Scan(&sq.Name, &sq.OwnerTable, &sq.OwnerColumn)
	}); err != nil {
		return nil, err
	}
	sort.SliceStable(s.Tables, func(i, j int) bool { return s.Tables[i].Name < s.Tables[j].Name })
	return s, nil
}

func trimUsing(s string) string {
	const p = " USING "
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):]
	}
	return s
}

func collect[T any](ctx context.Context, q Querier, sql, schema string, scan func(pgx.Rows) (T, error)) ([]T, error) {
	rows, err := q.Query(ctx, sql, schema)
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
