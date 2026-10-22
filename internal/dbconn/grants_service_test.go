package dbconn

import (
	"context"
	"errors"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

type fakeRepo struct {
	level authz.Level
	err   error
}

func (f *fakeRepo) Set(ctx context.Context, g, c int64, l authz.Level) (authz.Level, error) {
	prev := f.level
	if f.err == nil {
		f.level = l
	}
	return prev, f.err
}

func (f *fakeRepo) Remove(ctx context.Context, g, c int64) (authz.Level, error) {
	prev := f.level
	if f.err == nil {
		f.level = authz.LevelNone
	}
	return prev, f.err
}

type fakeNotifier struct{ rotated, revoked []int64 }

func (f *fakeNotifier) MarkRotateGroup(ctx context.Context, g int64) error {
	f.rotated = append(f.rotated, g)
	return nil
}

func (f *fakeNotifier) RevokeGroup(ctx context.Context, g int64) error {
	f.revoked = append(f.revoked, g)
	return nil
}

func TestSetAccessSessionEffects(t *testing.T) {
	tests := []struct {
		name         string
		prev, next   authz.Level
		rotate, kill bool
	}{
		{"grant baru view", authz.LevelNone, authz.LevelView, true, false},
		{"grant baru edit", authz.LevelNone, authz.LevelEdit, true, false},
		{"view naik ke edit", authz.LevelView, authz.LevelEdit, true, false},
		{"edit turun ke view", authz.LevelEdit, authz.LevelView, false, true},
		{"tidak berubah", authz.LevelView, authz.LevelView, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := &fakeNotifier{}
			s := &GrantService{Repo: &fakeRepo{level: tc.prev}, Sessions: n}
			if err := s.SetAccess(context.Background(), 7, 1, tc.next); err != nil {
				t.Fatal(err)
			}
			if (len(n.rotated) == 1) != tc.rotate || (len(n.revoked) == 1) != tc.kill {
				t.Fatalf("rotated=%v revoked=%v", n.rotated, n.revoked)
			}
			if len(n.rotated) == 1 && n.rotated[0] != 7 {
				t.Fatalf("group salah: %v", n.rotated)
			}
		})
	}
}

func TestSetAccessRejectsInvalidLevel(t *testing.T) {
	n := &fakeNotifier{}
	s := &GrantService{Repo: &fakeRepo{}, Sessions: n}
	if err := s.SetAccess(context.Background(), 1, 1, authz.LevelNone); !errors.Is(err, ErrInvalidLevel) {
		t.Fatalf("harus ErrInvalidLevel, dapat %v", err)
	}
	if err := s.SetAccess(context.Background(), 1, 1, authz.Level(99)); !errors.Is(err, ErrInvalidLevel) {
		t.Fatalf("harus ErrInvalidLevel, dapat %v", err)
	}
	if len(n.rotated)+len(n.revoked) != 0 {
		t.Fatal("level tidak valid tidak boleh menyentuh session")
	}
}

func TestSetAccessRepoErrorSkipsSessions(t *testing.T) {
	boom := errors.New("db down")
	n := &fakeNotifier{}
	s := &GrantService{Repo: &fakeRepo{err: boom}, Sessions: n}
	if err := s.SetAccess(context.Background(), 1, 1, authz.LevelEdit); !errors.Is(err, boom) {
		t.Fatalf("error repo harus diteruskan, dapat %v", err)
	}
	if len(n.rotated)+len(n.revoked) != 0 {
		t.Fatal("session tidak boleh disentuh bila simpan gagal")
	}
}

func TestRemoveAccess(t *testing.T) {
	n := &fakeNotifier{}
	s := &GrantService{Repo: &fakeRepo{level: authz.LevelView}, Sessions: n}
	if err := s.RemoveAccess(context.Background(), 3, 1); err != nil {
		t.Fatal(err)
	}
	if len(n.revoked) != 1 || n.revoked[0] != 3 {
		t.Fatalf("menghapus grant yang ada harus mencabut session: %v", n.revoked)
	}

	n2 := &fakeNotifier{}
	s2 := &GrantService{Repo: &fakeRepo{level: authz.LevelNone}, Sessions: n2}
	if err := s2.RemoveAccess(context.Background(), 3, 1); err != nil {
		t.Fatal(err)
	}
	if len(n2.revoked) != 0 {
		t.Fatal("menghapus grant yang tidak ada tidak boleh mencabut session")
	}
}
