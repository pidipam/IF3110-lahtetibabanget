package explorer

import (
	"context"
	"database/sql"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

const (
	qMySchemas = `SELECT schema_name FROM information_schema.schemata WHERE schema_name = DATABASE()`

	qMyTables = `SELECT table_schema, table_name, CASE table_type WHEN 'VIEW' THEN 'v' ELSE 'r' END
FROM information_schema.tables
WHERE table_schema = DATABASE() AND table_type IN ('BASE TABLE','VIEW')
ORDER BY table_name`

	qMyColumns = `SELECT table_schema, table_name, column_name, column_type, ordinal_position,
       is_nullable = 'YES', column_default
FROM information_schema.columns WHERE table_schema = DATABASE()
ORDER BY table_name, ordinal_position`

	qMyConstraints = `SELECT tc.table_schema, tc.table_name, tc.constraint_name, tc.constraint_type,
       COALESCE(GROUP_CONCAT(k.column_name ORDER BY k.ordinal_position SEPARATOR ','), ''),
       COALESCE(MAX(k.referenced_table_name), ''),
       COALESCE(GROUP_CONCAT(k.referenced_column_name ORDER BY k.ordinal_position SEPARATOR ','), ''),
       COALESCE(MAX(cc.check_clause), '')
FROM information_schema.table_constraints tc
LEFT JOIN information_schema.key_column_usage k
  ON k.constraint_schema = tc.constraint_schema AND k.table_schema = tc.table_schema
 AND k.table_name = tc.table_name AND k.constraint_name = tc.constraint_name
LEFT JOIN information_schema.check_constraints cc
  ON cc.constraint_schema = tc.constraint_schema AND cc.constraint_name = tc.constraint_name
WHERE tc.table_schema = DATABASE()
  AND tc.constraint_type IN ('PRIMARY KEY','UNIQUE','FOREIGN KEY','CHECK')
GROUP BY tc.table_schema, tc.table_name, tc.constraint_name, tc.constraint_type
ORDER BY tc.table_name, tc.constraint_name`

	qMyIndexes = `SELECT table_schema, table_name, index_name, MIN(non_unique) = 0,
       GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ',')
FROM information_schema.statistics WHERE table_schema = DATABASE()
GROUP BY table_schema, table_name, index_name
ORDER BY table_name, index_name`
)

func IntrospectSQL(ctx context.Context, db *sql.DB) (*Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, introspectTimeout)
	defer cancel()
	cat, err := introspectSQL(ctx, db)
	if err != nil {
		return nil, dbconn.Wrap(err)
	}
	return cat, nil
}

func splitCols(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func introspectSQL(ctx context.Context, db *sql.DB) (*Catalog, error) {
	schemas, err := collectSQL(ctx, db, qMySchemas, func(r *sql.Rows) (string, error) {
		var s string
		return s, r.Scan(&s)
	})
	if err != nil {
		return nil, err
	}
	tables, err := collectSQL(ctx, db, qMyTables, func(r *sql.Rows) (rawTable, error) {
		var t rawTable
		return t, r.Scan(&t.Schema, &t.Name, &t.Relkind)
	})
	if err != nil {
		return nil, err
	}
	cols, err := collectSQL(ctx, db, qMyColumns, func(r *sql.Rows) (rawColumn, error) {
		var c rawColumn
		var def sql.NullString
		if err := r.Scan(&c.Schema, &c.Table, &c.Name, &c.Type, &c.Position, &c.Nullable, &def); err != nil {
			return c, err
		}
		// MariaDB mengisi string "NULL" untuk kolom nullable tanpa default.
		if def.Valid && !(c.Nullable && def.String == "NULL") {
			d := def.String
			c.Default = &d
		}
		return c, nil
	})
	if err != nil {
		return nil, err
	}
	cons, err := collectSQL(ctx, db, qMyConstraints, func(r *sql.Rows) (rawConstraint, error) {
		var c rawConstraint
		var ctype, colList, refTable, refCols, check string
		if err := r.Scan(&c.Schema, &c.Table, &c.Name, &ctype, &colList, &refTable, &refCols, &check); err != nil {
			return c, err
		}
		c.Columns = splitCols(colList)
		switch ctype {
		case "PRIMARY KEY":
			c.Contype, c.Def = "p", "PRIMARY KEY ("+colList+")"
		case "UNIQUE":
			c.Contype, c.Def = "u", "UNIQUE ("+colList+")"
		case "FOREIGN KEY":
			c.Contype, c.Def = "f", "FOREIGN KEY ("+colList+") REFERENCES "+refTable+" ("+refCols+")"
		default:
			c.Contype, c.Def = "c", "CHECK "+check
		}
		return c, nil
	})
	if err != nil {
		return nil, err
	}
	idxs, err := collectSQL(ctx, db, qMyIndexes, func(r *sql.Rows) (rawIndex, error) {
		var i rawIndex
		var colList string
		if err := r.Scan(&i.Schema, &i.Table, &i.Name, &i.Unique, &colList); err != nil {
			return i, err
		}
		i.Primary = i.Name == "PRIMARY"
		kind := "INDEX"
		if i.Unique {
			kind = "UNIQUE INDEX"
		}
		i.Def = "CREATE " + kind + " " + QuoteIdentMySQL(i.Name) + " ON " + QuoteIdentMySQL(i.Table) + " (" + colList + ")"
		return i, nil
	})
	if err != nil {
		return nil, err
	}
	return assemble(schemas, tables, cols, cons, idxs), nil
}

func collectSQL[T any](ctx context.Context, db *sql.DB, q string, scan func(*sql.Rows) (T, error)) ([]T, error) {
	rows, err := db.QueryContext(ctx, q)
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
