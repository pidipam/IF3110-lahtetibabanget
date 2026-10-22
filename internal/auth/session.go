package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoSession = errors.New("session tidak ditemukan")

var ErrSessionExpired = errors.New("session berakhir")

type SessionConfig struct {
	IdleTimeout time.Duration
	AbsoluteTimeout time.Duration
	RotateInterval time.Duration
	GracePeriod time.Duration
}

type Session struct {
	ID int64
	UserID int64
	CSRFToken string
	CreatedAt time.Time
	LastSeenAt time.Time
	RotatedAt time.Time
	ExpiresAt time.Time
	ForceRotate bool
	Revoked bool
	IsCurrent bool
}

type SessionStore struct {
	db *pgxpool.Pool
	cfg SessionConfig
	Now func() time.Time
}

func NewSessionStore(db *pgxpool.Pool, cfg SessionConfig) *SessionStore {
	return &SessionStore{db: db, cfg: cfg, Now: time.Now}
}

func (s *SessionStore) Config() SessionConfig {
	return s.cfg
}

func CheckSession(sess *Session, now time.Time, cfg SessionConfig) error {
	if sess.Revoked {
		return ErrSessionExpired
	}
	if !now.Before(sess.ExpiresAt) {
		return ErrSessionExpired
	}
	if now.Sub(sess.LastSeenAt) > cfg.IdleTimeout {
		return ErrSessionExpired
	}
	return nil
}

func NeedsRotation(sess *Session, now time.Time, cfg SessionConfig) bool {
	if !sess.IsCurrent {
		return false
	}
	return sess.ForceRotate || now.Sub(sess.RotatedAt) >= cfg.RotateInterval
}

func (s *SessionStore) Create(ctx context.Context, userID int64) (string, *Session, error) {
	token, err := NewToken()
	if err != nil {
		return "", nil, err
	}
	csrf, err := NewToken()
	if err != nil {
		return "", nil, err
	}
	now := s.Now()
	sess := &Session{UserID: userID, CSRFToken: csrf, CreatedAt: now, LastSeenAt: now, RotatedAt: now, ExpiresAt: now.Add(s.cfg.AbsoluteTimeout), IsCurrent: true}
	err = s.db.QueryRow(ctx, "INSERT INTO sessions (user_id, token_hash, csrf_token, created_at, last_seen_at, rotated_at, expires_at) VALUES ($1, $2, $3, $4, $4, $4, $5) RETURNING id", userID, HashToken(token), csrf, now, sess.ExpiresAt).Scan(&sess.ID)
	if err != nil {
		return "", nil, err
	}
	return token, sess, nil
}

func (s *SessionStore) Lookup(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrNoSession
	}
	now := s.Now()
	h := HashToken(token)
	sess := &Session{}
	err := s.db.QueryRow(ctx, "SELECT id, user_id, csrf_token, created_at, last_seen_at, rotated_at, expires_at, force_rotate, revoked_at IS NOT NULL, token_hash = $1 FROM sessions WHERE token_hash = $1 OR (prev_token_hash = $1 AND prev_valid_until > $2)", h, now).Scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.CreatedAt, &sess.LastSeenAt, &sess.RotatedAt, &sess.ExpiresAt, &sess.ForceRotate, &sess.Revoked, &sess.IsCurrent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if err := CheckSession(sess, now, s.cfg); err != nil {
		if !sess.Revoked {
			s.Revoke(ctx, sess.ID)
		}
		return nil, err
	}
	return sess, nil
}

func (s *SessionStore) Touch(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "UPDATE sessions SET last_seen_at = $2 WHERE id = $1 AND revoked_at IS NULL", id, s.Now())
	return err
}

func (s *SessionStore) Rotate(ctx context.Context, sess *Session, presented string) (string, bool, error) {
	token, err := NewToken()
	if err != nil {
		return "", false, err
	}
	now := s.Now()
	tag, err := s.db.Exec(ctx, "UPDATE sessions SET prev_token_hash = token_hash, prev_valid_until = $3, token_hash = $4, rotated_at = $5, force_rotate = FALSE WHERE id = $1 AND token_hash = $2 AND revoked_at IS NULL", sess.ID, HashToken(presented), now.Add(s.cfg.GracePeriod), HashToken(token), now)
	if err != nil {
		return "", false, err
	}
	if tag.RowsAffected() == 0 {
		return "", false, nil
	}
	sess.RotatedAt = now
	sess.ForceRotate = false
	return token, true, nil
}

func (s *SessionStore) Revoke(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL", id, s.Now())
	return err
}

func (s *SessionStore) RevokeUser(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, "UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL", userID, s.Now())
	return err
}

func (s *SessionStore) RevokeGroup(ctx context.Context, groupID int64) error {
	_, err := s.db.Exec(ctx, "UPDATE sessions SET revoked_at = $2 WHERE revoked_at IS NULL AND user_id IN (SELECT user_id FROM group_members WHERE group_id = $1)", groupID, s.Now())
	return err
}

func (s *SessionStore) MarkRotateUser(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, "UPDATE sessions SET force_rotate = TRUE WHERE user_id = $1 AND revoked_at IS NULL", userID)
	return err
}

func (s *SessionStore) MarkRotateGroup(ctx context.Context, groupID int64) error {
	_, err := s.db.Exec(ctx, "UPDATE sessions SET force_rotate = TRUE WHERE revoked_at IS NULL AND user_id IN (SELECT user_id FROM group_members WHERE group_id = $1)", groupID)
	return err
}

func (s *SessionStore) DeleteExpired(ctx context.Context) error {
	_, err := s.db.Exec(ctx, "DELETE FROM sessions WHERE expires_at < $1 OR revoked_at < $1 - INTERVAL '1 day'", s.Now())
	return err
}