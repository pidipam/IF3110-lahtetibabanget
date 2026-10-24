package migration

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/platformdb"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

// testRepo menyiapkan platform DB bersih dan satu user ("alice"). Dipakai ulang oleh test berikutnya.
func testRepo(t *testing.T) (*Repository, *pgxpool.Pool, int64) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL belum diisi, integration test dilewati")
	}
	ctx := context.Background()
	db, err := platformdb.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := platformdb.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "TRUNCATE migrations, users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
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
	m, _ := r.Create(ctx, 1, uid, "a", "SELECT 1;")
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
	m, _ := r.Create(ctx, 1, uid, "lama", "SELECT 1;")
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
	db.Exec(ctx, "UPDATE migrations SET status = 'success', version = 1 WHERE id = $1", m.ID)
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
	first, _ := r.Create(ctx, 1, uid, "pertama", "SELECT 1;")
	second, _ := r.Create(ctx, 1, uid, "kedua", "SELECT 2;")
	r.Create(ctx, 2, uid, "db lain", "SELECT 3;")
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
}