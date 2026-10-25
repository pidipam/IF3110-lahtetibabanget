package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTargetUnavailable = errors.New("database target tidak dapat dihubungi")
	ErrTargetNotFound    = errors.New("database target tidak ditemukan")
)

// Targets adalah satu-satunya titik temu dengan fitur koneksi database (Orang B).
type Targets interface {
	Pool(ctx context.Context, dbID int64) (*pgxpool.Pool, error)
	Name(ctx context.Context, dbID int64) (string, error)
}

// StaticTargets melayani satu database target tetap. Dipakai untuk test dan
type StaticTargets struct {
	id   int64
	name string
	pool *pgxpool.Pool
}

type SQLTargets interface {
	Engine(ctx context.Context, dbID int64) (string, error)
	SQLDB(ctx context.Context, dbID int64) (*sql.DB, error)
}

func NewStaticTargets(ctx context.Context, id int64, name, url string) (*StaticTargets, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &StaticTargets{id: id, name: name, pool: pool}, nil
}

func (s *StaticTargets) Pool(ctx context.Context, dbID int64) (*pgxpool.Pool, error) {
	if dbID != s.id {
		return nil, ErrTargetNotFound
	}
	if err := s.pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTargetUnavailable, err)
	}
	return s.pool, nil
}

func (s *StaticTargets) Name(ctx context.Context, dbID int64) (string, error) {
	if dbID != s.id {
		return "", ErrTargetNotFound
	}
	return s.name, nil
}

func (s *StaticTargets) Close() { s.pool.Close() }
