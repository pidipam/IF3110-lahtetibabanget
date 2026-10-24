package dbconn_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func store(t *testing.T, db *pgxpool.Pool) *dbconn.Store {
	return dbconn.NewStore(db, testutil.NewCipher(t))
}

func validInput(name string) dbconn.Input {
	return dbconn.Input{Name: name, Host: "db.example.test", Port: 5432, DBName: "app", Username: "user", Password: "supersecret", SSLMode: "disable"}
}

func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestStoreCreateEncryptsPassword(t *testing.T) {
	db := testutil.NewTestDB(t)
	s := store(t, db)
	ctx := context.Background()
	c, err := s.Create(ctx, validInput("prod"))
	if err != nil {
		t.Fatal(err)
	}
	var enc string
	if err := db.QueryRow(ctx, "SELECT password_enc FROM db_connections WHERE id = $1", c.ID).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	if enc == "" || strings.Contains(enc, "supersecret") {
		t.Fatalf("password di DB harus terenkripsi, dapat %q", enc)
	}
	p, err := s.Credentials(ctx, c.ID)
	if err != nil || p.Password != "supersecret" || p.Host != "db.example.test" || p.Port != 5432 {
		t.Fatalf("Credentials: %v %v", p, err)
	}
}

func TestStoreUpdateKeepsPasswordWhenEmpty(t *testing.T) {
	db := testutil.NewTestDB(t)
	s := store(t, db)
	ctx := context.Background()
	c, _ := s.Create(ctx, validInput("prod"))

	in := validInput("prod-renamed")
	in.Password = ""
	in.Host = "other.example.test"
	updated, err := s.Update(ctx, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "prod-renamed" || updated.Host != "other.example.test" {
		t.Fatalf("field tidak berubah: %+v", updated)
	}
	p, _ := s.Credentials(ctx, c.ID)
	if p.Password != "supersecret" || p.Host != "other.example.test" {
		t.Fatalf("password lama harus dipertahankan: %+v", p)
	}

	in.Password = "newsecret"
	if _, err := s.Update(ctx, c.ID, in); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Credentials(ctx, c.ID); p.Password != "newsecret" {
		t.Fatalf("password baru tidak tersimpan: %q", p.Password)
	}
	if _, err := s.Update(ctx, 99999, in); !errors.Is(err, dbconn.ErrNotFound) {
		t.Fatalf("update id tak ada harus ErrNotFound, dapat %v", err)
	}
}

func TestStoreDuplicateNameAndValidation(t *testing.T) {
	db := testutil.NewTestDB(t)
	s := store(t, db)
	ctx := context.Background()
	s.Create(ctx, validInput("a"))
	b, _ := s.Create(ctx, validInput("b"))
	if _, err := s.Create(ctx, validInput("a")); !errors.Is(err, dbconn.ErrDuplicate) {
		t.Fatalf("nama kembar harus ErrDuplicate, dapat %v", err)
	}
	if _, err := s.Update(ctx, b.ID, validInput("a")); !errors.Is(err, dbconn.ErrDuplicate) {
		t.Fatalf("rename ke nama yang ada harus ErrDuplicate, dapat %v", err)
	}
	bad := validInput("c")
	bad.Port = 0
	var ve *dbconn.ValidationError
	if _, err := s.Create(ctx, bad); !errors.As(err, &ve) {
		t.Fatalf("port 0 harus ValidationError, dapat %v", err)
	}
	if n := testutil.Count(t, db, "SELECT count(*) FROM db_connections"); n != 2 {
		t.Fatalf("input tidak valid tidak boleh tersimpan, jumlah baris %d", n)
	}
}

func TestStoreGetListNotFound(t *testing.T) {
	db := testutil.NewTestDB(t)
	s := store(t, db)
	ctx := context.Background()
	if _, err := s.Get(ctx, 1); !errors.Is(err, dbconn.ErrNotFound) {
		t.Fatalf("Get harus ErrNotFound, dapat %v", err)
	}
	if err := s.Delete(ctx, 1); !errors.Is(err, dbconn.ErrNotFound) {
		t.Fatalf("Delete harus ErrNotFound, dapat %v", err)
	}
	if _, err := s.Credentials(ctx, 1); !errors.Is(err, dbconn.ErrNotFound) {
		t.Fatalf("Credentials harus ErrNotFound, dapat %v", err)
	}
	b := testutil.SeedConnection(t, db, func(in *dbconn.Input) { in.Name = "b" })
	a := testutil.SeedConnection(t, db, func(in *dbconn.Input) { in.Name = "a" })
	c := testutil.SeedConnection(t, db, func(in *dbconn.Input) { in.Name = "c" })
	list, err := s.List(ctx)
	if err != nil || len(list) != 3 || list[0].Name != "a" || list[2].Name != "c" {
		t.Fatalf("List harus terurut nama: %v %v", list, err)
	}
	sub, err := s.ListByIDs(ctx, []int64{c.ID, a.ID})
	if err != nil || len(sub) != 2 || sub[0].ID != a.ID {
		t.Fatalf("ListByIDs: %v %v", sub, err)
	}
	if none, err := s.ListByIDs(ctx, nil); err != nil || len(none) != 0 {
		t.Fatalf("ListByIDs kosong: %v %v", none, err)
	}
	_ = b
}

func TestStoreDeleteCascadesGrants(t *testing.T) {
	db := testutil.NewTestDB(t)
	conn := testutil.SeedConnection(t, db)
	g := testutil.SeedGroup(t, db, "tim")
	testutil.SeedGrant(t, db, g, conn.ID, authz.LevelView)
	if err := store(t, db).Delete(context.Background(), conn.ID); err != nil {
		t.Fatal(err)
	}
	if n := testutil.Count(t, db, "SELECT count(*) FROM group_db_access"); n != 0 {
		t.Fatalf("grant harus ikut terhapus, tersisa %d", n)
	}
	if n := testutil.Count(t, db, "SELECT count(*) FROM groups"); n != 1 {
		t.Fatal("group tidak boleh ikut terhapus")
	}
}

// Meniru tabel riwayat milik C: referensi ke db_connections tanpa CASCADE.
func TestStoreDeleteBlockedWhenReferenced(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, "CREATE TABLE fake_migrations (id serial PRIMARY KEY, connection_id bigint NOT NULL REFERENCES db_connections(id))"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(context.Background(), "DROP TABLE IF EXISTS fake_migrations") })

	conn := testutil.SeedConnection(t, db)
	g := testutil.SeedGroup(t, db, "tim")
	testutil.SeedGrant(t, db, g, conn.ID, authz.LevelEdit)
	db.Exec(ctx, "INSERT INTO fake_migrations (connection_id) VALUES ($1)", conn.ID)

	if err := store(t, db).Delete(ctx, conn.ID); !errors.Is(err, dbconn.ErrInUse) {
		t.Fatalf("harus ErrInUse, dapat %v", err)
	}
	if n := testutil.Count(t, db, "SELECT count(*) FROM db_connections"); n != 1 {
		t.Fatal("koneksi tidak boleh terhapus")
	}
	if n := testutil.Count(t, db, "SELECT count(*) FROM group_db_access"); n != 1 {
		t.Fatal("grant tidak boleh terhapus saat penghapusan diblokir")
	}
}

func TestGrantStoreSetRemoveList(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	gs := dbconn.NewGrantStore(db)
	c1 := testutil.SeedConnection(t, db)
	c2 := testutil.SeedConnection(t, db)
	g1 := testutil.SeedGroup(t, db, "alpha")
	g2 := testutil.SeedGroup(t, db, "beta")

	prev, err := gs.Set(ctx, g1, c1.ID, authz.LevelView)
	if err != nil || prev != authz.LevelNone {
		t.Fatalf("grant baru: prev=%v err=%v", prev, err)
	}
	prev, _ = gs.Set(ctx, g1, c1.ID, authz.LevelEdit)
	if prev != authz.LevelView {
		t.Fatalf("prev harus view, dapat %v", prev)
	}
	gs.Set(ctx, g2, c1.ID, authz.LevelView)
	gs.Set(ctx, g1, c2.ID, authz.LevelView)
	if n := testutil.Count(t, db, "SELECT count(*) FROM group_db_access"); n != 3 {
		t.Fatalf("upsert tidak boleh menggandakan baris: %d", n)
	}

	byConn, err := gs.ListByConnection(ctx, c1.ID)
	if err != nil || len(byConn) != 2 || byConn[0].GroupName != "alpha" || byConn[0].Level != authz.LevelEdit || byConn[1].GroupName != "beta" {
		t.Fatalf("ListByConnection: %+v %v", byConn, err)
	}
	byGroup, err := gs.ListByGroup(ctx, g1)
	if err != nil || len(byGroup) != 2 || byGroup[0].ConnID != c1.ID {
		t.Fatalf("ListByGroup: %+v %v", byGroup, err)
	}

	prev, err = gs.Remove(ctx, g1, c1.ID)
	if err != nil || prev != authz.LevelEdit {
		t.Fatalf("Remove: prev=%v err=%v", prev, err)
	}
	prev, err = gs.Remove(ctx, g1, c1.ID)
	if err != nil || prev != authz.LevelNone {
		t.Fatalf("Remove ulang harus no-op: prev=%v err=%v", prev, err)
	}

	if _, err := gs.Set(ctx, g1, 99999, authz.LevelView); !errors.Is(err, dbconn.ErrNotFound) {
		t.Fatalf("koneksi tak ada harus ErrNotFound, dapat %v", err)
	}
	if _, err := gs.Set(ctx, 99999, c1.ID, authz.LevelView); !errors.Is(err, dbconn.ErrNotFound) {
		t.Fatalf("group tak ada harus ErrNotFound, dapat %v", err)
	}
	if _, err := gs.Set(ctx, g1, c1.ID, authz.LevelNone); !errors.Is(err, dbconn.ErrInvalidLevel) {
		t.Fatalf("level none harus ErrInvalidLevel, dapat %v", err)
	}
}

// Menguji hook session dengan SessionStore asli milik A.
func TestGrantServiceAffectsRealSessions(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	sessions := auth.NewSessionStore(db, auth.SessionConfig{IdleTimeout: time.Hour, AbsoluteTimeout: 8 * time.Hour, RotateInterval: time.Hour, GracePeriod: time.Minute})
	svc := &dbconn.GrantService{Repo: dbconn.NewGrantStore(db), Sessions: sessions}

	member := testutil.SeedUser(t, db, "member", "password123", false)
	outsider := testutil.SeedUser(t, db, "outsider", "password123", false)
	g := testutil.SeedGroup(t, db, "tim")
	testutil.SeedMember(t, db, g, member)
	conn := testutil.SeedConnection(t, db)

	_, mem, err := sessions.Create(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	_, out, _ := sessions.Create(ctx, outsider)
	state := func(id int64) (force, revoked bool) {
		if err := db.QueryRow(ctx, "SELECT force_rotate, revoked_at IS NOT NULL FROM sessions WHERE id = $1", id).Scan(&force, &revoked); err != nil {
			t.Fatal(err)
		}
		return
	}

	// akses naik: session anggota ditandai rotasi, bukan dicabut
	if err := svc.SetAccess(ctx, g, conn.ID, authz.LevelView); err != nil {
		t.Fatal(err)
	}
	if f, r := state(mem.ID); !f || r {
		t.Fatalf("akses baru: force=%v revoked=%v", f, r)
	}
	if f, r := state(out.ID); f || r {
		t.Fatal("session non-anggota tidak boleh tersentuh")
	}

	// tidak berubah: tidak ada efek
	db.Exec(ctx, "UPDATE sessions SET force_rotate = false")
	if err := svc.SetAccess(ctx, g, conn.ID, authz.LevelView); err != nil {
		t.Fatal(err)
	}
	if f, r := state(mem.ID); f || r {
		t.Fatalf("tanpa perubahan: force=%v revoked=%v", f, r)
	}

	// akses turun: session anggota dicabut
	svc.SetAccess(ctx, g, conn.ID, authz.LevelEdit)
	db.Exec(ctx, "UPDATE sessions SET force_rotate = false")
	if err := svc.SetAccess(ctx, g, conn.ID, authz.LevelView); err != nil {
		t.Fatal(err)
	}
	if _, r := state(mem.ID); !r {
		t.Fatal("akses turun harus mencabut session anggota")
	}
	if _, r := state(out.ID); r {
		t.Fatal("session non-anggota tidak boleh dicabut")
	}

	// hapus akses: dicabut juga
	_, mem2, _ := sessions.Create(ctx, member)
	if err := svc.RemoveAccess(ctx, g, conn.ID); err != nil {
		t.Fatal(err)
	}
	if _, r := state(mem2.ID); !r {
		t.Fatal("hapus akses harus mencabut session anggota")
	}
}

func TestManagerConnectsToRealTargetAndRecovers(t *testing.T) {
	db := testutil.NewTestDB(t)
	cfg, err := pgx.ParseConfig(testutil.RequireEnv(t, testutil.EnvTestDB))
	if err != nil {
		t.Fatal(err)
	}
	s := store(t, db)
	ctx := context.Background()
	conn, err := s.Create(ctx, dbconn.Input{Name: "target", Host: cfg.Host, Port: int(cfg.Port), DBName: cfg.Database, Username: cfg.User, Password: cfg.Password, SSLMode: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	m := dbconn.NewManager(s)
	defer m.Close()

	pool, err := m.Get(ctx, conn.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("query lewat pool: %v", err)
	}
	if again, _ := m.Get(ctx, conn.ID); again != pool {
		t.Fatal("Get kedua harus memakai pool yang sama (cache)")
	}

	// Test() tidak menyimpan pool dan memakai kredensial dari form.
	p, _ := s.ParamsForEdit(ctx, conn.ID, dbconn.Input{Host: cfg.Host, Port: int(cfg.Port), DBName: cfg.Database, Username: cfg.User, SSLMode: "disable"})
	if err := m.Test(ctx, p); err != nil {
		t.Fatalf("Test dengan password tersimpan: %v", err)
	}

	// Admin mengubah koneksi ke port mati: setelah Invalidate, pool lama tidak dipakai lagi.
	in := dbconn.Input{Name: "target", Host: cfg.Host, Port: closedPort(t), DBName: cfg.Database, Username: cfg.User, SSLMode: "disable"}
	if _, err := s.Update(ctx, conn.ID, in); err != nil {
		t.Fatal(err)
	}
	m.Invalidate(conn.ID)
	_, err = m.Get(ctx, conn.ID)
	if !errors.Is(err, dbconn.ErrConnectionFailed) {
		t.Fatalf("target mati harus ErrConnectionFailed, dapat %v", err)
	}
	if ce, ok := dbconn.AsConnError(err); !ok || ce.Kind != dbconn.KindRefused {
		t.Fatalf("kind harus refused: %+v", ce)
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("pesan error tidak boleh memuat detail internal: %v", err)
	}

	// Koneksi dihapus: Get harus ErrNotFound.
	if err := s.Delete(ctx, conn.ID); err != nil {
		t.Fatal(err)
	}
	m.Invalidate(conn.ID)
	if _, err := m.Get(ctx, conn.ID); !errors.Is(err, dbconn.ErrNotFound) {
		t.Fatalf("setelah dihapus harus ErrNotFound, dapat %v", err)
	}
}
