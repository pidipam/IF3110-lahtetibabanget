package authz

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	levels map[int64][]Level
	err error
}

func (f fakeStore) GroupLevels(ctx context.Context, userID, dbID int64) ([]Level, error) {
	return f.levels[dbID], f.err
}

func (f fakeStore) DatabaseLevels(ctx context.Context, userID int64) (map[int64][]Level, error) {
	return f.levels, f.err
}

func TestEffectiveTakesHighest(t *testing.T) {
	cases := []struct {
		in []Level
		want Level
	}{{nil, LevelNone}, {[]Level{LevelView}, LevelView}, {[]Level{LevelView, LevelEdit}, LevelEdit}, {[]Level{LevelEdit, LevelView, LevelView}, LevelEdit}}
	for _, c := range cases {
		if got := Effective(c.in); got != c.want {
			t.Errorf("Effective(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAllows(t *testing.T) {
	if !Allows(LevelView, ViewDatabase) || !Allows(LevelView, ViewMigration) {
		t.Error("View Only harus bisa melihat database dan migration")
	}
	if Allows(LevelView, EditMigration) {
		t.Error("View Only tidak boleh mengubah migration")
	}
	if !Allows(LevelEdit, EditMigration) || !Allows(LevelEdit, ViewDatabase) {
		t.Error("View + Edit harus bisa melihat dan mengubah")
	}
	if Allows(LevelNone, ViewDatabase) {
		t.Error("tanpa akses tidak boleh melihat")
	}
}

func TestCan(t *testing.T) {
	a := New(fakeStore{levels: map[int64][]Level{1: {LevelView}, 2: {LevelView, LevelEdit}}})
	p := Principal{UserID: 10}
	ctx := context.Background()
	if err := a.Can(ctx, p, 1, ViewDatabase); err != nil {
		t.Errorf("view db 1: %v", err)
	}
	if err := a.Can(ctx, p, 1, EditMigration); !errors.Is(err, ErrForbidden) {
		t.Errorf("edit db 1: want ErrForbidden, got %v", err)
	}
	if err := a.Can(ctx, p, 2, EditMigration); err != nil {
		t.Errorf("edit db 2: %v", err)
	}
	if err := a.Can(ctx, p, 3, ViewDatabase); !errors.Is(err, ErrForbidden) {
		t.Errorf("db 3: want ErrForbidden, got %v", err)
	}
	if err := a.Can(ctx, p, 1, Action("hapus.semua")); !errors.Is(err, ErrUnknownAction) {
		t.Errorf("aksi asing: want ErrUnknownAction, got %v", err)
	}
}

func TestAdminIsNotDatabaseLevel(t *testing.T) {
	a := New(fakeStore{levels: map[int64][]Level{}})
	admin := Principal{UserID: 1, IsAdmin: true}
	ctx := context.Background()
	if err := a.Can(ctx, admin, 0, ManagePlatform); err != nil {
		t.Errorf("admin harus bisa mengelola platform: %v", err)
	}
	if err := a.Can(ctx, admin, 5, ViewDatabase); !errors.Is(err, ErrForbidden) {
		t.Errorf("admin tanpa group tidak otomatis punya akses database, got %v", err)
	}
	if err := a.Can(ctx, Principal{UserID: 2}, 0, ManagePlatform); !errors.Is(err, ErrForbidden) {
		t.Errorf("user biasa tidak boleh mengelola platform, got %v", err)
	}
}

func TestCanPropagatesStoreError(t *testing.T) {
	boom := errors.New("db mati")
	a := New(fakeStore{err: boom})
	if err := a.Can(context.Background(), Principal{UserID: 1}, 1, ViewDatabase); !errors.Is(err, boom) {
		t.Fatalf("want store error, got %v", err)
	}
}

func TestAccessibleDatabases(t *testing.T) {
	a := New(fakeStore{levels: map[int64][]Level{1: {LevelView}, 2: {LevelView, LevelEdit}, 3: {LevelNone}}})
	got, err := a.AccessibleDatabases(context.Background(), Principal{UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != LevelView || got[2] != LevelEdit {
		t.Fatalf("hasil salah: %v", got)
	}
}

func TestParseLevel(t *testing.T) {
	if ParseLevel("view") != LevelView || ParseLevel("edit") != LevelEdit || ParseLevel("apa") != LevelNone {
		t.Fatal("ParseLevel salah")
	}
	if LevelEdit.String() != "edit" || LevelView.Label() != "View Only" {
		t.Fatal("String/Label salah")
	}
}