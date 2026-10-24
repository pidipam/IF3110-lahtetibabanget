package users

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("data tidak ditemukan")

var ErrDuplicate = errors.New("nama sudah dipakai")

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	IsActive     bool
	CreatedAt    time.Time
}

type Group struct {
	ID          int64
	Name        string
	Description string
	MemberCount int
	CreatedAt   time.Time
}

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

const userColumns = "id, username, password_hash, is_admin, is_active, created_at"

func scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.IsActive, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func scanUsers(rows pgx.Rows, err error) ([]User, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *u)
	}
	return list, rows.Err()
}

func mapErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrDuplicate
	}
	return err
}

func (s *Store) GetByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE id = $1", id))
}

func (s *Store) GetByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE username = $1", username))
}

func (s *Store) List(ctx context.Context) ([]User, error) {
	return scanUsers(s.db.Query(ctx, "SELECT "+userColumns+" FROM users ORDER BY username"))
}

func (s *Store) Create(ctx context.Context, username, passwordHash string, isAdmin bool) (*User, error) {
	u, err := scanUser(s.db.QueryRow(ctx, "INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING "+userColumns, username, passwordHash, isAdmin))
	return u, mapErr(err)
}

func (s *Store) Update(ctx context.Context, id int64, username string, isAdmin, isActive bool) error {
	tag, err := s.db.Exec(ctx, "UPDATE users SET username = $2, is_admin = $3, is_active = $4, updated_at = now() WHERE id = $1", id, username, isAdmin, isActive)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetPassword(ctx context.Context, id int64, passwordHash string) error {
	tag, err := s.db.Exec(ctx, "UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1", id, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, "SELECT count(*) FROM users WHERE is_admin AND is_active").Scan(&n)
	return n, err
}

func (s *Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.Query(ctx, "SELECT g.id, g.name, g.description, g.created_at, count(m.user_id) FROM groups g LEFT JOIN group_members m ON m.group_id = g.id GROUP BY g.id ORDER BY g.name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.MemberCount); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, rows.Err()
}

func (s *Store) GetGroup(ctx context.Context, id int64) (*Group, error) {
	g := &Group{}
	err := s.db.QueryRow(ctx, "SELECT id, name, description, created_at FROM groups WHERE id = $1", id).Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *Store) CreateGroup(ctx context.Context, name, description string) (*Group, error) {
	g := &Group{Name: name, Description: description}
	err := s.db.QueryRow(ctx, "INSERT INTO groups (name, description) VALUES ($1, $2) RETURNING id, created_at", name, description).Scan(&g.ID, &g.CreatedAt)
	return g, mapErr(err)
}

func (s *Store) UpdateGroup(ctx context.Context, id int64, name, description string) error {
	tag, err := s.db.Exec(ctx, "UPDATE groups SET name = $2, description = $3 WHERE id = $1", id, name, description)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, "DELETE FROM groups WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Members(ctx context.Context, groupID int64) ([]User, error) {
	return scanUsers(s.db.Query(ctx, "SELECT u.id, u.username, u.password_hash, u.is_admin, u.is_active, u.created_at FROM users u JOIN group_members m ON m.user_id = u.id WHERE m.group_id = $1 ORDER BY u.username", groupID))
}

func (s *Store) NonMembers(ctx context.Context, groupID int64) ([]User, error) {
	return scanUsers(s.db.Query(ctx, "SELECT "+userColumns+" FROM users WHERE is_active AND id NOT IN (SELECT user_id FROM group_members WHERE group_id = $1) ORDER BY username", groupID))
}

func (s *Store) AddMember(ctx context.Context, groupID, userID int64) error {
	_, err := s.db.Exec(ctx, "INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", groupID, userID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrNotFound
	}
	return err
}

func (s *Store) RemoveMember(ctx context.Context, groupID, userID int64) error {
	_, err := s.db.Exec(ctx, "DELETE FROM group_members WHERE group_id = $1 AND user_id = $2", groupID, userID)
	return err
}

func (s *Store) GroupsOfUser(ctx context.Context, userID int64) ([]Group, error) {
	rows, err := s.db.Query(ctx, "SELECT g.id, g.name, g.description, g.created_at FROM groups g JOIN group_members m ON m.group_id = g.id WHERE m.user_id = $1 ORDER BY g.name", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, rows.Err()
}
