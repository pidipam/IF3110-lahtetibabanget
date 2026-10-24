package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
)

type managerTargets struct {
	db  *pgxpool.Pool
	get func(ctx context.Context, id int64) (*pgxpool.Pool, error)
}

func (t managerTargets) Pool(ctx context.Context, id int64) (*pgxpool.Pool, error) {
	pool, err := t.get(ctx, id)
	switch {
	case err == nil:
		return pool, nil
	case errors.Is(err, dbconn.ErrNotFound):
		return nil, migration.ErrTargetNotFound
	case errors.Is(err, dbconn.ErrConnectionFailed):
		return nil, fmt.Errorf("%w: %w", migration.ErrTargetUnavailable, err)
	}
	return nil, err
}

func (t managerTargets) Name(ctx context.Context, id int64) (string, error) {
	var name string
	err := t.db.QueryRow(ctx, "SELECT name FROM db_connections WHERE id = $1", id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", migration.ErrTargetNotFound
	}
	return name, err
}

// RecoverMigrations menandai migration yang tertinggal berstatus running sebagai failed.
func (a *App) RecoverMigrations(ctx context.Context) (int64, error) {
	if a.migrationExec == nil {
		return 0, nil
	}
	return a.migrationExec.RecoverStuck(ctx)
}
