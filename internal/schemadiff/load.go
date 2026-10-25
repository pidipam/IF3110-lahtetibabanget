package schemadiff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

const (
	previewTimeout = 25 * time.Second
	previewStatementTimeoutMS = "20000"
	previewLockTimeout        = "5s"
	scratchPrefix             = "zz_schemadiff_"
)

var (
	ErrSchemaNotFound = errors.New("schema tidak ditemukan")
	ErrEmptyDDL       = errors.New("DDL kondisi yang diinginkan wajib diisi")
)

type DDLError struct {
	Message string
	Err     error
}

func (e *DDLError) Error() string { return "DDL tidak valid: " + e.Message }

func (e *DDLError) Unwrap() error { return e.Err }

type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

const qUserSchemas = `SELECT n.nspname FROM pg_namespace n
WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
ORDER BY n.nspname`

func Schemas(ctx context.Context, q Querier) ([]string, error) {
	rows, err := q.Query(ctx, qUserSchemas)
	if err != nil {
		return nil, dbconn.Wrap(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, dbconn.Wrap(err)
	}
	return out, nil
}

func Load(ctx context.Context, db TxBeginner, schema string) (*Schema, error) {
	if !validSchemaName(schema) {
		return nil, ErrSchemaNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, dbconn.Wrap(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if err := prepareTx(ctx, tx); err != nil {
		return nil, err
	}
	if err := requireSchema(ctx, tx, schema); err != nil {
		return nil, err
	}
	if err := setSearchPath(ctx, tx, schema, "public", "pg_catalog"); err != nil {
		return nil, err
	}
	return Introspect(ctx, tx, schema)
}

func LoadDDL(ctx context.Context, db TxBeginner, schema, ddl string) (*Schema, error) {
	if !validSchemaName(schema) {
		return nil, ErrSchemaNotFound
	}
	if strings.TrimSpace(ddl) == "" {
		return nil, ErrEmptyDDL
	}
	if strings.ContainsRune(ddl, 0) || !utf8.ValidString(ddl) {
		return nil, &DDLError{Message: "DDL memuat karakter tidak valid."}
	}

	name, err := newScratchSchema()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, dbconn.Wrap(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if err := prepareTx(ctx, tx); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+QuoteIdent(name)); err != nil {
		return nil, fmt.Errorf("membuat schema sementara: %w", dbconn.Wrap(err))
	}
	if err := setSearchPath(ctx, tx, name, schema, "public", "pg_catalog"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, ddl, pgx.QueryExecModeSimpleProtocol); err != nil {
		return nil, asDDLError(err)
	}
	if err := setSearchPath(ctx, tx, name, schema, "public", "pg_catalog"); err != nil {
		return nil, err
	}
	out, err := Introspect(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	out.Name = schema
	return out, nil
}

func prepareTx(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = "+previewStatementTimeoutMS); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '"+previewLockTimeout+"'"); err != nil {
		return err
	}
	return nil
}

func requireSchema(ctx context.Context, tx pgx.Tx, schema string) error {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_namespace
		WHERE nspname = $1 AND nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
	)`, schema).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrSchemaNotFound
	}
	return nil
}

func setSearchPath(ctx context.Context, tx pgx.Tx, names ...string) error {
	seen := map[string]bool{}
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		quoted = append(quoted, QuoteIdent(name))
	}
	_, err := tx.Exec(ctx, "SET LOCAL search_path TO "+strings.Join(quoted, ", "))
	return err
}

func validSchemaName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	return name != "information_schema" && !strings.HasPrefix(name, "pg_")
}

func newScratchSchema() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return scratchPrefix + hex.EncodeToString(b), nil
}

func asDDLError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Message
		if pgErr.Hint != "" {
			msg += " " + pgErr.Hint
		}
		return &DDLError{Message: truncateMessage(msg, 500), Err: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &DDLError{Message: "DDL melewati batas waktu preview.", Err: err}
	}
	return &DDLError{Message: "DDL gagal dijalankan pada database target.", Err: err}
}

func truncateMessage(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
