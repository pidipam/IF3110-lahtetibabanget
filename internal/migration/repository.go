package migration

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("migration tidak ditemukan")

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

const selectMigration = `SELECT m.id, m.db_id, m.version, m.title, m.sql_text, m.status,
	m.created_by, m.executed_by, cu.username, COALESCE(eu.username, ''),
	m.created_at, m.updated_at, m.started_at, m.finished_at, m.error_message
	FROM migrations m
	JOIN users cu ON cu.id = m.created_by
	LEFT JOIN users eu ON eu.id = m.executed_by`

func scanMigration(row pgx.Row) (*Migration, error) {
	m := &Migration{}
	var status string
	err := row.Scan(&m.ID, &m.DBID, &m.Version, &m.Title, &m.SQL, &status,
		&m.CreatedBy, &m.ExecutedBy, &m.CreatedByName, &m.ExecutedByName,
		&m.CreatedAt, &m.UpdatedAt, &m.StartedAt, &m.FinishedAt, &m.ErrorMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Status = Status(status)
	return m, nil
}

// Create menyimpan migration baru sebagai draft (belum punya versi).
func (r *Repository) Create(ctx context.Context, dbID, userID int64, title, sql string) (*Migration, error) {
	var id int64
	err := r.db.QueryRow(ctx,
		"INSERT INTO migrations (db_id, title, sql_text, status, created_by) VALUES ($1, $2, $3, 'draft', $4) RETURNING id",
		dbID, title, sql, userID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, dbID, id)
}

// Get selalu menyaring db_id sehingga migration milik database lain tidak bisa dibuka lewat tebakan ID.
func (r *Repository) Get(ctx context.Context, dbID, id int64) (*Migration, error) {
	return scanMigration(r.db.QueryRow(ctx, selectMigration+" WHERE m.id = $1 AND m.db_id = $2", id, dbID))
}

// List mengembalikan riwayat satu database, terbaru di atas.
func (r *Repository) List(ctx context.Context, dbID int64) ([]Migration, error) {
	rows, err := r.db.Query(ctx, selectMigration+" WHERE m.db_id = $1 ORDER BY m.created_at DESC, m.id DESC", dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Migration
	for rows.Next() {
		m, err := scanMigration(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *m)
	}
	return list, rows.Err()
}

// UpdateDraft hanya berhasil pada migration berstatus draft.
func (r *Repository) UpdateDraft(ctx context.Context, dbID, id int64, title, sql string) error {
	tag, err := r.db.Exec(ctx,
		"UPDATE migrations SET title = $3, sql_text = $4, updated_at = now() WHERE id = $1 AND db_id = $2 AND status = 'draft'",
		id, dbID, title, sql)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := r.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM migrations WHERE id = $1 AND db_id = $2)", id, dbID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return ErrNotDraft
}
