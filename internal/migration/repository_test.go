package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

// testRepo menyiapkan platform DB bersih, dua koneksi (id 1 dan 2), dan satu user ("alice").
func testRepo(t *testing.T) (*Repository, *pgxpool.Pool, int64) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test dilewati pada -short")
	}
	db := testutil.NewTestDB(t) // migrasi platform + TRUNCATE ... RESTART IDENTITY
	ctx := context.Background()
	for _, name := range []string{"mig-test-1", "mig-test-2"} {
		if _, err := db.Exec(ctx, `INSERT INTO db_connections (name, host, port, db_name, username, password_enc)
			VALUES ($1, '127.0.0.1', 5435, 'sample', 'app', 'x')`, name); err != nil {
			t.Fatal(err)
		}
	}
	u, err := users.NewStore(db).Create(ctx, "alice", "bukan-hash-asli", false)
	if err != nil {
		t.Fatal(err)
	}
	return NewRepository(db), db, u.ID
}

func TestCreateAndGet(t *testing.T) {
	r, _, uid := testRepo(t)
	ctx := context.Background()
	m, err := r.Create(ctx, 1, uid, "tambah tabel", "CREATE TABLE t (id int);")
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != StatusDraft || m.Version != nil || m.ExecutedBy != nil {
		t.Fatalf("draft baru salah: %+v", m)
	}
	if m.CreatedByName != "alice" || m.ExecutedByName != "" || m.Title != "tambah tabel" {
		t.Fatalf("data tidak sesuai: %+v", m)
	}
	got, err := r.Get(ctx, 1, m.ID)
	if err != nil || got.SQL != "CREATE TABLE t (id int);" {
		t.Fatalf("Get gagal: %v %+v", err, got)
	}
}

func TestGetScopedToDatabase(t *testing.T) {
	r, _, uid := testRepo(t)
	ctx := context.Background()
	m, err := r.Create(ctx, 1, uid, "a", "SELECT 1;")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, 2, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("migration milik DB 1 tidak boleh terbuka lewat DB 2, got %v", err)
	}
	if _, err := r.Get(ctx, 1, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ID asing harus ErrNotFound, got %v", err)
	}
}

func TestUpdateDraft(t *testing.T) {
	r, db, uid := testRepo(t)
	ctx := context.Background()
	m, err := r.Create(ctx, 1, uid, "lama", "SELECT 1;")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateDraft(ctx, 1, m.ID, "baru", "SELECT 2;"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, 1, m.ID)
	if got.Title != "baru" || got.SQL != "SELECT 2;" {
		t.Fatalf("update tidak tersimpan: %+v", got)
	}
	if err := r.UpdateDraft(ctx, 2, m.ID, "x", "SELECT 3;"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DB salah harus ErrNotFound, got %v", err)
	}
	if _, err := db.Exec(ctx, "UPDATE migrations SET status = 'success', version = 1 WHERE id = $1", m.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateDraft(ctx, 1, m.ID, "x", "SELECT 4;"); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("non-draft harus ErrNotDraft, got %v", err)
	}
	got, _ = r.Get(ctx, 1, m.ID)
	if got.Title != "baru" {
		t.Fatal("migration non-draft berubah")
	}
}

func TestListScopedAndNewestFirst(t *testing.T) {
	r, _, uid := testRepo(t)
	ctx := context.Background()
	first, err := r.Create(ctx, 1, uid, "pertama", "SELECT 1;")
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Create(ctx, 1, uid, "kedua", "SELECT 2;")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(ctx, 2, uid, "db lain", "SELECT 3;"); err != nil {
		t.Fatal(err)
	}
	list, err := r.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("daftar salah: %+v", list)
	}
	if empty, err := r.List(ctx, 99); err != nil || len(empty) != 0 {
		t.Fatalf("DB tanpa riwayat harus kosong: %v %v", empty, err)
	}
}

func TestSchemaConstraints(t *testing.T) {
	_, db, uid := testRepo(t)
	ctx := context.Background()
	const q = "INSERT INTO migrations (db_id, version, title, sql_text, status, created_by) VALUES ($1, $2, 't', 's', $3, $4)"
	if _, err := db.Exec(ctx, q, 1, 1, "draft", uid); err == nil {
		t.Error("draft dengan versi harus ditolak")
	}
	if _, err := db.Exec(ctx, q, 1, nil, "success", uid); err == nil {
		t.Error("success tanpa versi harus ditolak")
	}
	if _, err := db.Exec(ctx, q, 1, 1, "aneh", uid); err == nil {
		t.Error("status asing harus ditolak")
	}
	if _, err := db.Exec(ctx, q, 1, 1, "success", uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, q, 1, 1, "failed", uid); err == nil {
		t.Error("versi ganda pada DB yang sama harus ditolak")
	}
	if _, err := db.Exec(ctx, q, 2, 1, "success", uid); err != nil {
		t.Errorf("versi 1 pada DB lain harus boleh: %v", err)
	}
	if _, err := db.Exec(ctx, q, 99, nil, "draft", uid); err == nil {
		t.Error("db_id tanpa koneksi harus ditolak oleh foreign key")
	}
}

func TestConnectionWithHistoryCannotBeDeleted(t *testing.T) {
	r, db, uid := testRepo(t)
	ctx := context.Background()
	if _, err := r.Create(ctx, 1, uid, "riwayat", "SELECT 1;"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "DELETE FROM db_connections WHERE id = 1"); err == nil {
		t.Fatal("koneksi yang punya riwayat migration tidak boleh terhapus (FK harus RESTRICT, bukan CASCADE)")
	}
	if _, err := db.Exec(ctx, "DELETE FROM db_connections WHERE id = 2"); err != nil {
		t.Fatalf("koneksi tanpa riwayat harus bisa dihapus: %v", err)
	}
}
