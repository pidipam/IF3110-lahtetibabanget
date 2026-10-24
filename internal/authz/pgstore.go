package authz

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGStore struct {
	db *pgxpool.Pool
}

func NewPGStore(db *pgxpool.Pool) *PGStore {
	return &PGStore{db: db}
}

func (s *PGStore) GroupLevels(ctx context.Context, userID, dbID int64) ([]Level, error) {
	rows, err := s.db.Query(ctx, "SELECT a.level FROM group_db_access a JOIN group_members m ON m.group_id = a.group_id JOIN users u ON u.id = m.user_id WHERE m.user_id = $1 AND a.db_id = $2 AND u.is_active", userID, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var levels []Level
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		levels = append(levels, ParseLevel(s))
	}
	return levels, rows.Err()
}

func (s *PGStore) DatabaseLevels(ctx context.Context, userID int64) (map[int64][]Level, error) {
	rows, err := s.db.Query(ctx, "SELECT a.db_id, a.level FROM group_db_access a JOIN group_members m ON m.group_id = a.group_id JOIN users u ON u.id = m.user_id WHERE m.user_id = $1 AND u.is_active", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Level{}
	for rows.Next() {
		var dbID int64
		var s string
		if err := rows.Scan(&dbID, &s); err != nil {
			return nil, err
		}
		out[dbID] = append(out[dbID], ParseLevel(s))
	}
	return out, rows.Err()
}
