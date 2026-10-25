package migration

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

var ErrExplainUnsupported = fmt.Errorf("analisis execution plan belum didukung untuk MySQL/MariaDB")

type session interface {
	Run(ctx context.Context, sqlText string, timeout time.Duration) error // commit bila sukses
	Close()                                                               // rollback bila belum commit, lalu lepas koneksi
}

func (e *Executor) openSession(ctx context.Context, dbID int64) (session, error) {
	if et, ok := e.targets.(SQLTargets); ok {
		eng, err := et.Engine(ctx, dbID)
		if err != nil {
			return nil, err
		}
		if dbconn.IsMySQLFamily(eng) {
			db, err := et.SQLDB(ctx, dbID)
			if err != nil {
				return nil, err
			}
			return newMySQLSession(ctx, db)
		}
	}
	pool, err := e.targets.Pool(ctx, dbID)
	if err != nil {
		return nil, err
	}
	return newPGSession(ctx, pool)
}

type pgSession struct {
	conn *pgxpool.Conn
	tx   pgx.Tx
}

func newPGSession(ctx context.Context, pool *pgxpool.Pool) (session, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTargetUnavailable, dbconn.Wrap(err))
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("%w: %w", ErrTargetUnavailable, dbconn.Wrap(err))
	}
	return &pgSession{conn: conn, tx: tx}, nil
}

func (s *pgSession) Run(ctx context.Context, sqlText string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	if _, err := s.tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeout.Milliseconds())); err != nil {
		return err
	}
	if _, err := s.tx.Exec(ctx, "SET LOCAL lock_timeout = '10s'"); err != nil {
		return err
	}
	if _, err := s.tx.Exec(ctx, sqlText, pgx.QueryExecModeSimpleProtocol); err != nil {
		return err
	}
	return s.tx.Commit(ctx)
}

func (s *pgSession) Close() {
	s.tx.Rollback(context.Background()) // no-op bila sudah commit
	s.conn.Release()
}

type mysqlSession struct {
	conn *sql.Conn
	tx   *sql.Tx
}

func newMySQLSession(ctx context.Context, db *sql.DB) (session, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTargetUnavailable, dbconn.Wrap(err))
	}
	// Di luar transaction agar tidak mengganggu; direset saat Close.
	if _, err := conn.ExecContext(ctx, "SET SESSION lock_wait_timeout = 10"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: %w", ErrTargetUnavailable, dbconn.Wrap(err))
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: %w", ErrTargetUnavailable, dbconn.Wrap(err))
	}
	return &mysqlSession{conn: conn, tx: tx}, nil
}

func (s *mysqlSession) Run(ctx context.Context, sqlText string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stmts := SplitStatements(sqlText)
	for i, st := range stmts {
		if _, err := s.tx.ExecContext(ctx, st); err != nil {
			if i > 0 {
				return fmt.Errorf("statement %d gagal: %w. Statement 1-%d mungkin sudah diterapkan karena DDL di MySQL/MariaDB tidak dapat di-rollback", i+1, err, i)
			}
			return fmt.Errorf("statement %d gagal: %w", i+1, err)
		}
	}
	return s.tx.Commit()
}

func (s *mysqlSession) Close() {
	s.tx.Rollback()
	s.conn.ExecContext(context.Background(), "SET SESSION lock_wait_timeout = DEFAULT")
	s.conn.Close() // mengembalikan koneksi ke pool
}
