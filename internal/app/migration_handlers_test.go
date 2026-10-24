package app

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"bytes"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/platformdb"
	"golang.org/x/crypto/bcrypt"
)

func setupMig(t *testing.T) *testEnv {
	t.Helper()
	dsn, target := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_TARGET_DATABASE_URL")
	if dsn == "" || target == "" {
		t.Skip("TEST_DATABASE_URL / TEST_TARGET_DATABASE_URL belum diisi, test dilewati")
	}
	auth.BcryptCost = bcrypt.MinCost
	ctx := context.Background()
	db, err := platformdb.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := platformdb.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "TRUNCATE users, groups, group_members, sessions, group_db_access, migrations RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	targets, err := migration.NewStaticTargets(ctx, 1, "target-test", target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(targets.Close)
	env := &testEnv{t: t, db: db, now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), log: &bytes.Buffer{}}
	a, err := New(Options{DB: db, Session: testSession, Logger: slog.New(slog.NewTextHandler(env.log, nil)), Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	a.Sessions.Now = func() time.Time { return env.now }
	env.app = a
	return env
}

func (e *testEnv) grantAccess(groupID, dbID int64, level string) {
	e.t.Helper()
	if _, err := e.db.Exec(context.Background(), "INSERT INTO group_db_access (group_id, db_id, level) VALUES ($1, $2, $3)", groupID, dbID, level); err != nil {
		e.t.Fatal(err)
	}
}

func TestMigrationPagesAccessControl(t *testing.T) {
	e := setupMig(t)
	ctx := context.Background()
	editor := e.createUser("editor", "password123", false)
	viewer := e.createUser("viewer", "password123", false)
	e.createUser("outsider", "password123", false)
	ge, _ := e.app.Users.CreateGroup(ctx, "editors", "")
	gv, _ := e.app.Users.CreateGroup(ctx, "viewers", "")
	e.app.Users.AddMember(ctx, ge.ID, editor.ID)
	e.app.Users.AddMember(ctx, gv.ID, viewer.ID)
	e.grantAccess(ge.ID, 1, "edit")
	e.grantAccess(gv.ID, 1, "view")

	// Anonim diarahkan ke login.
	anon := e.client().do("GET", "/databases/1/migrations", nil)
	expectStatus(t, anon, http.StatusSeeOther)

	// Editor: halaman terbuka, CSRF wajib, draft tersimpan.
	ed := e.client()
	expectStatus(t, ed.login("editor", "password123"), http.StatusSeeOther)
	page := ed.do("GET", "/databases/1/migrations", nil)
	expectStatus(t, page, http.StatusOK)
	if !strings.Contains(page.Body.String(), "Buat migration") {
		t.Fatal("editor harus melihat tombol Buat migration")
	}
	form := url.Values{"title": {"tabel halaman"}, "sql": {"CREATE TABLE mig_h1 (id int);"}}
	expectStatus(t, ed.do("POST", "/databases/1/migrations", form), http.StatusForbidden) // tanpa CSRF
	w := ed.post("/databases/1/migrations", form)
	expectStatus(t, w, http.StatusSeeOther)
	loc := w.Header().Get("Location")
	path := strings.SplitN(loc, "?", 2)[0]
	if !strings.HasPrefix(path, "/databases/1/migrations/") {
		t.Fatalf("redirect ke %q", loc)
	}
	detail := ed.do("GET", loc, nil)
	expectStatus(t, detail, http.StatusOK)
	if !strings.Contains(detail.Body.String(), "CREATE TABLE mig_h1") || !strings.Contains(detail.Body.String(), "berhasil disimpan") {
		t.Fatal("detail tidak menampilkan SQL atau notifikasi")
	}
	expectStatus(t, ed.post(path, url.Values{"title": {"judul baru"}, "sql": {"SELECT 1;"}}), http.StatusSeeOther)
	if !strings.Contains(ed.do("GET", path, nil).Body.String(), "judul baru") {
		t.Fatal("edit draft tidak tersimpan")
	}
	bad := ed.post("/databases/1/migrations", url.Values{"title": {" "}, "sql": {"SELECT 1;"}})
	expectStatus(t, bad, http.StatusUnprocessableEntity)
	if !strings.Contains(bad.Body.String(), "judul wajib diisi") {
		t.Fatal("pesan validasi tidak tampil")
	}
	expectStatus(t, ed.do("GET", "/databases/2/migrations", nil), http.StatusForbidden) // tanpa grant

	// View Only: boleh melihat, tidak boleh mengubah (dicek di backend).
	v := e.client()
	expectStatus(t, v.login("viewer", "password123"), http.StatusSeeOther)
	vlist := v.do("GET", "/databases/1/migrations", nil)
	expectStatus(t, vlist, http.StatusOK)
	if strings.Contains(vlist.Body.String(), "Buat migration") {
		t.Fatal("View Only tidak boleh melihat tombol Buat migration")
	}
	expectStatus(t, v.do("GET", path, nil), http.StatusOK)
	expectStatus(t, v.do("GET", "/databases/1/migrations/new", nil), http.StatusForbidden)
	expectStatus(t, v.do("GET", path+"/edit", nil), http.StatusForbidden)
	expectStatus(t, v.post("/databases/1/migrations", form), http.StatusForbidden)
	expectStatus(t, v.post(path, url.Values{"title": {"retas"}, "sql": {"DROP TABLE x;"}}), http.StatusForbidden)
	if e.count("SELECT count(*) FROM migrations") != 1 {
		t.Fatal("View Only berhasil menambah migration")
	}
	if e.count("SELECT count(*) FROM migrations WHERE title = 'retas'") != 0 {
		t.Fatal("View Only berhasil mengubah migration")
	}

	// Tanpa grup: ditolak.
	o := e.client()
	expectStatus(t, o.login("outsider", "password123"), http.StatusSeeOther)
	expectStatus(t, o.do("GET", "/databases/1/migrations", nil), http.StatusForbidden)
	expectStatus(t, o.do("GET", path, nil), http.StatusForbidden)
}
