package authz

import (
	"context"
	"errors"
)

type Action string

const (
	ManagePlatform Action = "platform.manage"
	ViewDatabase   Action = "database.view"
	ViewMigration  Action = "migration.view"
	EditMigration  Action = "migration.edit"
)

type Level int

const (
	LevelNone Level = iota
	LevelView
	LevelEdit
)

var ErrForbidden = errors.New("akses ditolak")

var ErrUnknownAction = errors.New("aksi tidak dikenal")

type Principal struct {
	UserID  int64
	IsAdmin bool
}

type Store interface {
	GroupLevels(ctx context.Context, userID, dbID int64) ([]Level, error)
	DatabaseLevels(ctx context.Context, userID int64) (map[int64][]Level, error)
}

var requiredLevel = map[Action]Level{ViewDatabase: LevelView, ViewMigration: LevelView, EditMigration: LevelEdit}

func ParseLevel(s string) Level {
	switch s {
	case "view":
		return LevelView
	case "edit":
		return LevelEdit
	}
	return LevelNone
}

func (l Level) String() string {
	switch l {
	case LevelView:
		return "view"
	case LevelEdit:
		return "edit"
	}
	return "none"
}

func (l Level) Label() string {
	switch l {
	case LevelView:
		return "View Only"
	case LevelEdit:
		return "View + Edit"
	}
	return "Tidak ada akses"
}

func Effective(levels []Level) Level {
	best := LevelNone
	for _, l := range levels {
		if l > best {
			best = l
		}
	}
	return best
}

func Allows(level Level, action Action) bool {
	need, ok := requiredLevel[action]
	return ok && level >= need
}

type Authorizer struct {
	store Store
}

func New(store Store) *Authorizer {
	return &Authorizer{store: store}
}

func (a *Authorizer) CanManagePlatform(p Principal) error {
	if !p.IsAdmin {
		return ErrForbidden
	}
	return nil
}

func (a *Authorizer) Level(ctx context.Context, p Principal, dbID int64) (Level, error) {
	levels, err := a.store.GroupLevels(ctx, p.UserID, dbID)
	if err != nil {
		return LevelNone, err
	}
	return Effective(levels), nil
}

func (a *Authorizer) Can(ctx context.Context, p Principal, dbID int64, action Action) error {
	if action == ManagePlatform {
		return a.CanManagePlatform(p)
	}
	if _, ok := requiredLevel[action]; !ok {
		return ErrUnknownAction
	}
	level, err := a.Level(ctx, p, dbID)
	if err != nil {
		return err
	}
	if !Allows(level, action) {
		return ErrForbidden
	}
	return nil
}

func (a *Authorizer) AccessibleDatabases(ctx context.Context, p Principal) (map[int64]Level, error) {
	all, err := a.store.DatabaseLevels(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Level, len(all))
	for dbID, levels := range all {
		if l := Effective(levels); l > LevelNone {
			out[dbID] = l
		}
	}
	return out, nil
}
