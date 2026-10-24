package dbconn

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

// membaca dan menulis tabel group_db_access
type GrantStore struct {
	db *pgxpool.Pool
}

func NewGrantStore(db *pgxpool.Pool) *GrantStore {
	return &GrantStore{db: db}
}

func (g *GrantStore) Set(ctx context.Context, groupID, connID int64, level authz.Level) (authz.Level, error) {
	if level != authz.LevelView && level != authz.LevelEdit {
		return authz.LevelNone, ErrInvalidLevel
	}
	tx, err := g.db.Begin(ctx)
	if err != nil {
		return authz.LevelNone, err
	}
	defer tx.Rollback(ctx)

	prev := authz.LevelNone
	var cur string
	err = tx.QueryRow(ctx, "SELECT level FROM group_db_access WHERE group_id = $1 AND db_id = $2 FOR UPDATE", groupID, connID).Scan(&cur)
	switch {
	case err == nil:
		prev = authz.ParseLevel(cur)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return authz.LevelNone, err
	}

	_, err = tx.Exec(ctx, "INSERT INTO group_db_access (group_id, db_id, level) VALUES ($1, $2, $3) ON CONFLICT (group_id, db_id) DO UPDATE SET level = EXCLUDED.level", groupID, connID, level.String())
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return authz.LevelNone, ErrNotFound // group atau koneksi tidak ada
		}
		return authz.LevelNone, err
	}
	if err := tx.Commit(ctx); err != nil {
		return authz.LevelNone, err
	}
	return prev, nil
}

func (g *GrantStore) Remove(ctx context.Context, groupID, connID int64) (authz.Level, error) {
	var cur string
	err := g.db.QueryRow(ctx, "DELETE FROM group_db_access WHERE group_id = $1 AND db_id = $2 RETURNING level", groupID, connID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.LevelNone, nil
	}
	if err != nil {
		return authz.LevelNone, err
	}
	return authz.ParseLevel(cur), nil
}

func (g *GrantStore) ListByConnection(ctx context.Context, connID int64) ([]Grant, error) {
	return g.list(ctx, "WHERE a.db_id = $1 ORDER BY gr.name", connID)
}

func (g *GrantStore) ListByGroup(ctx context.Context, groupID int64) ([]Grant, error) {
	return g.list(ctx, "WHERE a.group_id = $1 ORDER BY c.name", groupID)
}

func (g *GrantStore) list(ctx context.Context, tail string, arg int64) ([]Grant, error) {
	rows, err := g.db.Query(ctx, "SELECT a.group_id, gr.name, a.db_id, c.name, a.level FROM group_db_access a JOIN groups gr ON gr.id = a.group_id JOIN db_connections c ON c.id = a.db_id "+tail, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var gr Grant
		var level string
		if err := rows.Scan(&gr.GroupID, &gr.GroupName, &gr.ConnID, &gr.ConnName, &level); err != nil {
			return nil, err
		}
		gr.Level = authz.ParseLevel(level)
		out = append(out, gr)
	}
	return out, rows.Err()
}
