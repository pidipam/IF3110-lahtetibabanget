package dbconn

import (
	"context"
	"errors"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

var ErrInvalidLevel = errors.New("level akses tidak valid")

type Grant struct {
	GroupID   int64
	GroupName string
	ConnID    int64
	ConnName  string
	Level     authz.Level
}

type GrantRepo interface {
	Set(ctx context.Context, groupID, connID int64, level authz.Level) (authz.Level, error)
	Remove(ctx context.Context, groupID, connID int64) (authz.Level, error)
}

// dipenuhi oleh *auth.SessionStore
type SessionNotifier interface {
	MarkRotateGroup(ctx context.Context, groupID int64) error
	RevokeGroup(ctx context.Context, groupID int64) error
}

type GrantService struct {
	Repo     GrantRepo
	Sessions SessionNotifier
}

func (s *GrantService) SetAccess(ctx context.Context, groupID, connID int64, level authz.Level) error {
	if level != authz.LevelView && level != authz.LevelEdit {
		return ErrInvalidLevel
	}
	prev, err := s.Repo.Set(ctx, groupID, connID, level)
	if err != nil {
		return err
	}
	switch {
	case level > prev:
		return s.Sessions.MarkRotateGroup(ctx, groupID)
	case level < prev:
		return s.Sessions.RevokeGroup(ctx, groupID)
	}
	return nil
}

func (s *GrantService) RemoveAccess(ctx context.Context, groupID, connID int64) error {
	prev, err := s.Repo.Remove(ctx, groupID, connID)
	if err != nil {
		return err
	}
	if prev > authz.LevelNone {
		return s.Sessions.RevokeGroup(ctx, groupID)
	}
	return nil
}
