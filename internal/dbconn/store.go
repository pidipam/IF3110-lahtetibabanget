package dbconn

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const connColumns = "id, name, host, port, db_name, username, sslmode, engine, created_at, updated_at"

type Store struct {
	db     *pgxpool.Pool
	cipher *Cipher
}

func NewStore(db *pgxpool.Pool, c *Cipher) *Store {
	return &Store{db: db, cipher: c}
}

func scanConnection(row pgx.Row) (*Connection, error) {
	c := &Connection{}
	err := row.Scan(&c.ID, &c.Name, &c.Host, &c.Port, &c.DBName, &c.Username, &c.SSLMode, &c.Engine, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func mapStoreErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrDuplicate
		case "23503":
			return ErrInUse
		}
	}
	return err
}

func (s *Store) Create(ctx context.Context, in Input) (*Connection, error) {
	in.Normalize()
	if err := in.Validate(true); err != nil {
		return nil, err
	}
	enc, err := s.cipher.Encrypt(in.Password)
	if err != nil {
		return nil, fmt.Errorf("enkripsi password: %w", err)
	}
	c, err := scanConnection(s.db.QueryRow(ctx,
		"INSERT INTO db_connections (name, host, port, db_name, username, password_enc, sslmode, engine) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING "+connColumns,
		in.Name, in.Host, in.Port, in.DBName, in.Username, enc, in.SSLMode, in.Engine))
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return c, nil
}

// panggil Manager.Invalidate(id) setelah berhasil
func (s *Store) Update(ctx context.Context, id int64, in Input) (*Connection, error) {
	in.Normalize()
	if err := in.Validate(false); err != nil {
		return nil, err
	}
	var enc *string
	if in.Password != "" {
		e, err := s.cipher.Encrypt(in.Password)
		if err != nil {
			return nil, fmt.Errorf("enkripsi password: %w", err)
		}
		enc = &e
	}
	c, err := scanConnection(s.db.QueryRow(ctx,
		"UPDATE db_connections SET name = $2, host = $3, port = $4, db_name = $5, username = $6, sslmode = $7, password_enc = COALESCE($8::text, password_enc), updated_at = now() WHERE id = $1 RETURNING "+connColumns,
		id, in.Name, in.Host, in.Port, in.DBName, in.Username, in.SSLMode, enc))
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return c, nil
}

// panggil Manager.Invalidate(id) setelah berhasil
func (s *Store) Delete(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, "DELETE FROM db_connections WHERE id = $1", id)
	if err != nil {
		return mapStoreErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id int64) (*Connection, error) {
	c, err := scanConnection(s.db.QueryRow(ctx, "SELECT "+connColumns+" FROM db_connections WHERE id = $1", id))
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return c, nil
}

func (s *Store) List(ctx context.Context) ([]*Connection, error) {
	rows, err := s.db.Query(ctx, "SELECT "+connColumns+" FROM db_connections ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Connection
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// id dari authz.AccessibleDatabases
func (s *Store) ListByIDs(ctx context.Context, ids []int64) ([]*Connection, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, "SELECT "+connColumns+" FROM db_connections WHERE id = ANY($1) ORDER BY name", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Connection
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// jangan dikirim ke template
func (s *Store) Credentials(ctx context.Context, id int64) (Params, error) {
	var p Params
	var enc string
	err := s.db.QueryRow(ctx, "SELECT host, port, db_name, username, password_enc, sslmode, engine FROM db_connections WHERE id = $1", id).
		Scan(&p.Host, &p.Port, &p.DBName, &p.Username, &enc, &p.SSLMode, &p.Engine)
	if err != nil {
		return Params{}, mapStoreErr(err)
	}
	pw, err := s.cipher.Decrypt(enc)
	if err != nil {
		return Params{}, err
	}
	p.Password = pw
	return p, nil
}

func ParamsFromInput(in Input) Params {
	in.Normalize()
	return Params{Host: in.Host, Port: in.Port, DBName: in.DBName, Username: in.Username, Password: in.Password, SSLMode: in.SSLMode, Engine: in.Engine}
}

func (s *Store) ParamsForEdit(ctx context.Context, id int64, in Input) (Params, error) {
	p := ParamsFromInput(in)
	old, err := s.Credentials(ctx, id)
	if err != nil {
		return Params{}, err
	}
	p.Engine = old.Engine
	if in.Password == "" {
		p.Password = old.Password
	}
	return p, nil
}
