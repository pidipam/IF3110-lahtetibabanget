package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

// fakeAccess: dbID -> level efektif user yang sedang diuji.
type fakeAccess map[int64]authz.Level

func (f fakeAccess) GroupLevels(ctx context.Context, userID, dbID int64) ([]authz.Level, error) {
	if l, ok := f[dbID]; ok {
		return []authz.Level{l}, nil
	}
	return nil, nil
}

func (f fakeAccess) DatabaseLevels(ctx context.Context, userID int64) (map[int64][]authz.Level, error) {
	out := map[int64][]authz.Level{}
	for id, l := range f {
		out[id] = []authz.Level{l}
	}
	return out, nil
}

func (x *execEnv) service(access fakeAccess) *Service {
	return NewService(x.repo, x.e, authz.New(access))
}

func TestServiceViewOnly(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	svc := x.service(fakeAccess{1: authz.LevelView})
	p := authz.Principal{UserID: x.uid}
	m := x.draft(t, "CREATE TABLE mig_t1 (id int);")

	if _, err := svc.List(ctx, p, 1); err != nil {
		t.Errorf("View Only harus bisa melihat riwayat: %v", err)
	}
	if got, err := svc.Get(ctx, p, 1, m.ID); err != nil || got.ID != m.ID {
		t.Errorf("View Only harus bisa melihat detail: %v", err)
	}
	if _, err := svc.Create(ctx, p, 1, "judul", "SELECT 1;"); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("Create harus ditolak, got %v", err)
	}
	if err := svc.Update(ctx, p, 1, m.ID, "ubah", "SELECT 2;"); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("Update harus ditolak, got %v", err)
	}
	if _, err := svc.Execute(ctx, p, 1, m.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("Execute harus ditolak, got %v", err)
	}
	after, _ := x.repo.Get(ctx, 1, m.ID)
	if after.Status != StatusDraft || after.Version != nil || after.Title != "uji" {
		t.Fatalf("penolakan tidak boleh mengubah data: %+v", after)
	}
	if x.tableExists(t, "mig_t1") {
		t.Fatal("SQL dijalankan padahal user hanya View Only")
	}
	if list, _ := x.repo.List(ctx, 1); len(list) != 1 {
		t.Fatalf("Create yang ditolak tidak boleh menyimpan data, ada %d", len(list))
	}
}

func TestServiceNoAccess(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	svc := x.service(fakeAccess{2: authz.LevelEdit}) // punya akses ke DB 2, bukan DB 1
	p := authz.Principal{UserID: x.uid}
	m := x.draft(t, "SELECT 1;")

	if _, err := svc.List(ctx, p, 1); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("List tanpa akses: %v", err)
	}
	if _, err := svc.Get(ctx, p, 1, m.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("Get tanpa akses: %v", err)
	}
	if _, err := svc.Execute(ctx, p, 1, m.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("Execute tanpa akses: %v", err)
	}
	if _, err := svc.TargetName(ctx, p, 1); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("TargetName tanpa akses: %v", err)
	}
}

func TestServiceAdminWithoutGrantForbidden(t *testing.T) {
	x := setupExec(t)
	svc := x.service(fakeAccess{})
	admin := authz.Principal{UserID: x.uid, IsAdmin: true}
	if _, err := svc.List(context.Background(), admin, 1); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("Platform Admin bukan level akses database, got %v", err)
	}
}

func TestServiceEditFullFlow(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	svc := x.service(fakeAccess{1: authz.LevelEdit})
	p := authz.Principal{UserID: x.uid}

	m, err := svc.Create(ctx, p, 1, "  buat tabel  ", "CREATE TABLE mig_t1 (id int);")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "buat tabel" || m.Status != StatusDraft {
		t.Fatalf("draft salah: %+v", m)
	}
	if err := svc.Update(ctx, p, 1, m.ID, "buat tabel 1", "CREATE TABLE mig_t1 (id int, nama text);"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Execute(ctx, p, 1, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSuccess || *got.Version != 1 || got.Title != "buat tabel 1" {
		t.Fatalf("hasil salah: %+v", got)
	}
	if !x.tableExists(t, "mig_t1") {
		t.Fatal("tabel tidak terbentuk")
	}
	if err := svc.Update(ctx, p, 1, m.ID, "x", "SELECT 1;"); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("edit setelah dijalankan harus ErrNotDraft, got %v", err)
	}
	name, err := svc.TargetName(ctx, p, 1)
	if err != nil || name != "target-test" {
		t.Fatalf("TargetName: %q %v", name, err)
	}
}

func TestServiceValidationRejectsBadInput(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	svc := x.service(fakeAccess{1: authz.LevelEdit})
	p := authz.Principal{UserID: x.uid}

	cases := []struct {
		name, title, sql string
		want             error
	}{
		{"judul kosong", "  ", "SELECT 1;", ErrEmptyTitle},
		{"sql kosong", "t", "   ", ErrEmptySQL},
		{"commit", "t", "SELECT 1; COMMIT;", ErrTxControl},
	}
	for _, c := range cases {
		if _, err := svc.Create(ctx, p, 1, c.title, c.sql); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	if list, _ := x.repo.List(ctx, 1); len(list) != 0 {
		t.Fatalf("input tidak valid tidak boleh tersimpan, ada %d", len(list))
	}
}

func TestServiceCannotReachOtherDatabase(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	// Punya Edit di DB 1 dan DB 2, tetapi migration milik DB 1.
	svc := x.service(fakeAccess{1: authz.LevelEdit, 2: authz.LevelEdit})
	p := authz.Principal{UserID: x.uid}
	m := x.draft(t, "SELECT 1;")

	if _, err := svc.Get(ctx, p, 2, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get lintas DB: %v", err)
	}
	if err := svc.Update(ctx, p, 2, m.ID, "x", "SELECT 2;"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update lintas DB: %v", err)
	}
	if _, err := svc.Execute(ctx, p, 2, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Execute lintas DB: %v", err)
	}
}