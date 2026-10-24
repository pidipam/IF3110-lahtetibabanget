package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// migConn membuat satu baris db_connections. Halaman riwayat/editor hanya membaca nama koneksi
// lewat SQL, jadi password_enc palsu cukup. Eksekusi sungguhan (butuh Manager) diuji terpisah.
func migConn(e *testEnv, name string) int64 {
	e.t.Helper()
	var id int64
	err := e.db.QueryRow(context.Background(), `INSERT INTO db_connections (name, host, port, db_name, username, password_enc)
		VALUES ($1, '127.0.0.1', 5435, 'sample', 'app', 'x') RETURNING id`, name).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func migGrant(e *testEnv, groupID, connID int64, level string) {
	e.t.Helper()
	if _, err := e.db.Exec(context.Background(),
		"INSERT INTO group_db_access (group_id, db_id, level) VALUES ($1, $2, $3)", groupID, connID, level); err != nil {
		e.t.Fatal(err)
	}
}

// migFixture: dua koneksi, user editor (View + Edit) dan viewer (View Only) pada koneksi pertama,
// user outsider tanpa grup, dan admin tanpa grant.
type migFixture struct {
	e     *testEnv
	conn1 int64
	conn2 int64
	base  string // /databases/{conn1}/migrations
}

func setupMigPages(t *testing.T) *migFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test dilewati pada -short")
	}
	e := setup(t)
	ctx := context.Background()
	// Pastikan tabel baru bersih walaupun setup(t) hanya mengosongkan tabel miliknya.
	if _, err := e.db.Exec(ctx, "TRUNCATE migrations, db_connections RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	f := &migFixture{e: e, conn1: migConn(e, "mig-db-1"), conn2: migConn(e, "mig-db-2")}
	f.base = fmt.Sprintf("/databases/%d/migrations", f.conn1)

	editor := e.createUser("editor", "password123", false)
	viewer := e.createUser("viewer", "password123", false)
	e.createUser("outsider", "password123", false)
	e.createUser("admin", "password123", true)
	ge, err := e.app.Users.CreateGroup(ctx, "editors", "")
	if err != nil {
		t.Fatal(err)
	}
	gv, err := e.app.Users.CreateGroup(ctx, "viewers", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.app.Users.AddMember(ctx, ge.ID, editor.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Users.AddMember(ctx, gv.ID, viewer.ID); err != nil {
		t.Fatal(err)
	}
	migGrant(e, ge.ID, f.conn1, "edit")
	migGrant(e, gv.ID, f.conn1, "view")
	return f
}

func TestMigrationPagesEditorFlow(t *testing.T) {
	f := setupMigPages(t)
	e := f.e
	ed := e.client()
	expectStatus(t, ed.login("editor", "password123"), http.StatusSeeOther)

	page := ed.do("GET", f.base, nil)
	expectStatus(t, page, http.StatusOK)
	if !strings.Contains(page.Body.String(), "Buat migration") {
		t.Fatal("editor harus melihat tombol Buat migration")
	}
	expectStatus(t, ed.do("GET", f.base+"/new", nil), http.StatusOK)

	form := url.Values{"title": {"tabel halaman"}, "sql": {"CREATE TABLE zz_mig_h1 (id int);"}}
	expectStatus(t, ed.do("POST", f.base, form), http.StatusForbidden) // tanpa CSRF
	if e.count("SELECT count(*) FROM migrations") != 0 {
		t.Fatal("draft tersimpan tanpa CSRF")
	}

	w := ed.post(f.base, form)
	expectStatus(t, w, http.StatusSeeOther)
	loc := w.Header().Get("Location")
	path := strings.SplitN(loc, "?", 2)[0]
	if !strings.HasPrefix(path, f.base+"/") {
		t.Fatalf("redirect ke %q", loc)
	}
	detail := ed.do("GET", loc, nil)
	expectStatus(t, detail, http.StatusOK)
	if !strings.Contains(detail.Body.String(), "CREATE TABLE zz_mig_h1") || !strings.Contains(detail.Body.String(), "berhasil disimpan") {
		t.Fatal("detail tidak menampilkan SQL atau notifikasi")
	}
	if e.count("SELECT count(*) FROM migrations WHERE status = 'draft' AND version IS NULL") != 1 {
		t.Fatal("draft baru harus berstatus draft tanpa versi")
	}

	expectStatus(t, ed.do("GET", path+"/edit", nil), http.StatusOK)
	expectStatus(t, ed.post(path, url.Values{"title": {"judul baru"}, "sql": {"SELECT 1;"}}), http.StatusSeeOther)
	if !strings.Contains(ed.do("GET", path, nil).Body.String(), "judul baru") {
		t.Fatal("edit draft tidak tersimpan")
	}

	bad := ed.post(f.base, url.Values{"title": {" "}, "sql": {"SELECT 1;"}})
	expectStatus(t, bad, http.StatusUnprocessableEntity)
	if !strings.Contains(bad.Body.String(), "judul wajib diisi") {
		t.Fatal("pesan validasi judul tidak tampil")
	}
	tx := ed.post(f.base, url.Values{"title": {"x"}, "sql": {"SELECT 1; COMMIT;"}})
	expectStatus(t, tx, http.StatusUnprocessableEntity)
	if e.count("SELECT count(*) FROM migrations") != 1 {
		t.Fatal("input tidak valid tidak boleh tersimpan")
	}

	// Migration yang sudah berstatus final tidak bisa diedit.
	if _, err := e.db.Exec(context.Background(), "UPDATE migrations SET status = 'success', version = 1"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, ed.do("GET", path+"/edit", nil), http.StatusConflict)
	expectStatus(t, ed.post(path, url.Values{"title": {"x"}, "sql": {"SELECT 2;"}}), http.StatusConflict)
	if !strings.Contains(ed.do("GET", f.base, nil).Body.String(), "judul baru") {
		t.Fatal("riwayat harus menampilkan migration")
	}
}

func TestMigrationPagesViewOnly(t *testing.T) {
	f := setupMigPages(t)
	e := f.e
	ctx := context.Background()
	ed := e.client()
	expectStatus(t, ed.login("editor", "password123"), http.StatusSeeOther)
	w := ed.post(f.base, url.Values{"title": {"milik editor"}, "sql": {"SELECT 1;"}})
	expectStatus(t, w, http.StatusSeeOther)
	path := strings.SplitN(w.Header().Get("Location"), "?", 2)[0]

	v := e.client()
	expectStatus(t, v.login("viewer", "password123"), http.StatusSeeOther)
	list := v.do("GET", f.base, nil)
	expectStatus(t, list, http.StatusOK)
	if strings.Contains(list.Body.String(), "Buat migration") {
		t.Fatal("View Only tidak boleh melihat tombol Buat migration")
	}
	if !strings.Contains(list.Body.String(), "milik editor") {
		t.Fatal("View Only harus bisa melihat riwayat")
	}
	expectStatus(t, v.do("GET", path, nil), http.StatusOK) // detail boleh

	// Semua perubahan ditolak di backend, bukan hanya disembunyikan.
	expectStatus(t, v.do("GET", f.base+"/new", nil), http.StatusForbidden)
	expectStatus(t, v.do("GET", path+"/edit", nil), http.StatusForbidden)
	expectStatus(t, v.post(f.base, url.Values{"title": {"retas"}, "sql": {"SELECT 1;"}}), http.StatusForbidden)
	expectStatus(t, v.post(path, url.Values{"title": {"retas"}, "sql": {"DROP TABLE x;"}}), http.StatusForbidden)
	if e.count("SELECT count(*) FROM migrations") != 1 {
		t.Fatal("View Only berhasil menambah migration")
	}
	if e.count("SELECT count(*) FROM migrations WHERE title = 'retas'") != 0 {
		t.Fatal("View Only berhasil mengubah migration")
	}

	// Setelah akses diturunkan, tombol hilang dan perubahan ditolak, tanpa harus login ulang.
	_ = ctx
}

func TestMigrationPagesDenyWithoutGrant(t *testing.T) {
	f := setupMigPages(t)
	e := f.e

	// Anonim diarahkan ke login.
	anon := e.client().do("GET", f.base, nil)
	expectStatus(t, anon, http.StatusSeeOther)
	if anon.Header().Get("Location") != "/login" {
		t.Fatalf("anonim diarahkan ke %q", anon.Header().Get("Location"))
	}

	ed := e.client()
	expectStatus(t, ed.login("editor", "password123"), http.StatusSeeOther)
	w := ed.post(f.base, url.Values{"title": {"rahasia"}, "sql": {"SELECT 1;"}})
	expectStatus(t, w, http.StatusSeeOther)
	path := strings.SplitN(w.Header().Get("Location"), "?", 2)[0]

	// User tanpa grup.
	o := e.client()
	expectStatus(t, o.login("outsider", "password123"), http.StatusSeeOther)
	expectStatus(t, o.do("GET", f.base, nil), http.StatusForbidden)
	expectStatus(t, o.do("GET", path, nil), http.StatusForbidden)
	expectStatus(t, o.post(f.base, url.Values{"title": {"x"}, "sql": {"SELECT 1;"}}), http.StatusForbidden)

	// Platform Admin bukan level akses database.
	a := e.client()
	expectStatus(t, a.login("admin", "password123"), http.StatusSeeOther)
	expectStatus(t, a.do("GET", f.base, nil), http.StatusForbidden)
	expectStatus(t, a.post(f.base, url.Values{"title": {"x"}, "sql": {"SELECT 1;"}}), http.StatusForbidden)

	// Editor punya akses ke koneksi 1 saja: koneksi 2 dan ID yang tidak ada sama-sama 403
	// (agar ID tidak bisa ditebak); format ID salah 404.
	expectStatus(t, ed.do("GET", fmt.Sprintf("/databases/%d/migrations", f.conn2), nil), http.StatusForbidden)
	expectStatus(t, ed.do("GET", "/databases/9999/migrations", nil), http.StatusForbidden)
	expectStatus(t, ed.do("GET", "/databases/abc/migrations", nil), http.StatusNotFound)

	// Migration milik koneksi 1 tidak bisa dibuka lewat koneksi 2 walau user punya akses di sana.
	gid := e.userGroup("editor")
	migGrant(e, gid, f.conn2, "edit")
	other := fmt.Sprintf("/databases/%d/migrations/", f.conn2) + path[strings.LastIndex(path, "/")+1:]
	expectStatus(t, ed.do("GET", other, nil), http.StatusNotFound)
}

// userGroup mengembalikan id grup pertama milik user (dipakai test di atas).
func (e *testEnv) userGroup(username string) int64 {
	e.t.Helper()
	var id int64
	err := e.db.QueryRow(context.Background(), `SELECT m.group_id FROM group_members m
		JOIN users u ON u.id = m.user_id WHERE u.username = $1 ORDER BY m.group_id LIMIT 1`, username).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}
