package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

const secretPW = "pw-rahasia-Zq9x7"

const closedPort = 5499

func connValues(name string) url.Values {
	return url.Values{"name": {name}, "host": {"127.0.0.1"}, "port": {"5435"}, "db_name": {"sample"}, "username": {"app"}, "password": {secretPW}, "sslmode": {"disable"}}
}

func (e *testEnv) loginAs(username string, admin bool) *client {
	e.t.Helper()
	e.createUser(username, "password123", admin)
	c := e.client()
	expectStatus(e.t, c.login(username, "password123"), http.StatusSeeOther)
	return c
}

func (e *testEnv) userID(username string) int64 {
	e.t.Helper()
	u, err := e.app.Users.GetByUsername(context.Background(), username)
	if err != nil {
		e.t.Fatal(err)
	}
	return u.ID
}

func (e *testEnv) noSecretInLogs() {
	e.t.Helper()
	if strings.Contains(e.log.String(), secretPW) {
		e.t.Fatal("log memuat password koneksi")
	}
}

func TestAdminConnectionCRUD(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.loginAs("admin", true)

	w := admin.post("/admin/connections", connValues("sampel"))
	expectStatus(t, w, http.StatusSeeOther)
	if loc := w.Header().Get("Location"); loc != "/admin/connections?ok=connection_created" {
		t.Fatalf("redirect ke %q", loc)
	}
	var id int64
	var enc string
	if err := e.db.QueryRow(ctx, "SELECT id, password_enc FROM db_connections WHERE name = 'sampel'").Scan(&id, &enc); err != nil {
		t.Fatal(err)
	}
	if enc == "" || strings.Contains(enc, secretPW) {
		t.Fatalf("password harus tersimpan terenkripsi, bukan plaintext: %q", enc)
	}

	list := admin.do("GET", "/admin/connections?ok=connection_created", nil)
	expectStatus(t, list, http.StatusOK)
	for _, want := range []string{"sampel", "127.0.0.1:5435", "Koneksi database berhasil dibuat."} {
		if !strings.Contains(list.Body.String(), want) {
			t.Fatalf("daftar koneksi tidak memuat %q", want)
		}
	}
	edit := admin.do("GET", fmt.Sprintf("/admin/connections/%d", id), nil)
	expectStatus(t, edit, http.StatusOK)
	if !strings.Contains(edit.Body.String(), "sampel") {
		t.Fatal("halaman edit tidak menampilkan koneksi")
	}
	for _, body := range []string{list.Body.String(), edit.Body.String()} {
		if strings.Contains(body, secretPW) || strings.Contains(body, enc) {
			t.Fatal("password atau ciphertext bocor ke halaman")
		}
	}

	v := connValues("sampel-baru")
	v.Set("password", "")
	expectStatus(t, admin.post(fmt.Sprintf("/admin/connections/%d", id), v), http.StatusSeeOther)
	creds, err := e.app.Conns.Credentials(ctx, id)
	if err != nil || creds.Password != secretPW {
		t.Fatalf("password kosong harus mempertahankan password lama: %v", err)
	}
	if e.count("SELECT count(*) FROM db_connections WHERE name = 'sampel-baru'") != 1 {
		t.Fatal("nama koneksi tidak berubah")
	}
	v.Set("password", "password-baru")
	expectStatus(t, admin.post(fmt.Sprintf("/admin/connections/%d", id), v), http.StatusSeeOther)
	if creds, _ := e.app.Conns.Credentials(ctx, id); creds.Password != "password-baru" {
		t.Fatal("password baru tidak tersimpan")
	}

	expectStatus(t, admin.post(fmt.Sprintf("/admin/connections/%d/delete", id), nil), http.StatusSeeOther)
	if e.count("SELECT count(*) FROM db_connections") != 0 {
		t.Fatal("koneksi tidak terhapus")
	}
	expectStatus(t, admin.do("GET", fmt.Sprintf("/admin/connections/%d", id), nil), http.StatusNotFound)
	expectStatus(t, admin.post(fmt.Sprintf("/admin/connections/%d/delete", id), nil), http.StatusNotFound)
	expectStatus(t, admin.post("/admin/connections/abc", connValues("x")), http.StatusNotFound)
	e.noSecretInLogs()
}

func TestAdminConnectionValidation(t *testing.T) {
	e := setup(t)
	admin := e.loginAs("admin", true)
	set := func(k, v string) url.Values { f := connValues("valid"); f.Set(k, v); return f }
	cases := []struct {
		name string
		form url.Values
		msg  string
	}{
		{"nama kosong", set("name", "  "), "Nama koneksi wajib diisi"},
		{"nama terlalu panjang", set("name", strings.Repeat("a", 65)), "maksimal 64"},
		{"host kosong", set("host", ""), "Host tidak valid."},
		{"host berisi simbol", set("host", "a/b@c"), "Host tidak valid."},
		{"port bukan angka", set("port", "abc"), "Port harus antara 1 dan 65535."},
		{"port nol", set("port", "0"), "Port harus antara 1 dan 65535."},
		{"port terlalu besar", set("port", "70000"), "Port harus antara 1 dan 65535."},
		{"database kosong", set("db_name", ""), "Nama database wajib diisi"},
		{"username kosong", set("username", ""), "Username wajib diisi"},
		{"password kosong", set("password", ""), "Password wajib diisi."},
		{"sslmode salah", set("sslmode", "bebas"), "SSL mode tidak valid."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := admin.post("/admin/connections", tc.form)
			expectStatus(t, w, http.StatusUnprocessableEntity)
			if !strings.Contains(w.Body.String(), tc.msg) {
				t.Fatalf("pesan %q tidak ada", tc.msg)
			}
			if strings.Contains(w.Body.String(), secretPW) {
				t.Fatal("password diisi ulang ke form")
			}
		})
	}
	if e.count("SELECT count(*) FROM db_connections") != 0 {
		t.Fatal("koneksi tidak valid tersimpan")
	}
	expectStatus(t, admin.post("/admin/connections", connValues("kembar")), http.StatusSeeOther)
	w := admin.post("/admin/connections", connValues("kembar"))
	expectStatus(t, w, http.StatusUnprocessableEntity)
	if !strings.Contains(w.Body.String(), "Nama koneksi sudah dipakai.") {
		t.Fatal("pesan nama kembar tidak ada")
	}
	other := testutil.SeedConnection(t, e.db)
	w = admin.post(fmt.Sprintf("/admin/connections/%d", other.ID), connValues("kembar"))
	expectStatus(t, w, http.StatusUnprocessableEntity)
	if e.count("SELECT count(*) FROM db_connections") != 2 {
		t.Fatal("jumlah koneksi berubah")
	}
	e.noSecretInLogs()
}

func TestAdminConnectionTestButton(t *testing.T) {
	e := setup(t)
	admin := e.loginAs("admin", true)

	w := admin.post("/admin/connections/test", connValues("baru"))
	expectStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Test koneksi berhasil.") {
		t.Fatal("test koneksi yang benar harus berhasil")
	}
	if e.count("SELECT count(*) FROM db_connections") != 0 {
		t.Fatal("test koneksi tidak boleh menyimpan apa pun")
	}

	bad := connValues("baru")
	bad.Set("port", fmt.Sprint(closedPort))
	w = admin.post("/admin/connections/test", bad)
	expectStatus(t, w, http.StatusOK)
	body := w.Body.String()
	if !strings.Contains(body, "Test koneksi gagal.") || !strings.Contains(body, dbconn.KindRefused.UserMessage()) {
		t.Fatalf("pesan gagal ramah tidak ada: %s", body)
	}
	if strings.Contains(body, secretPW) {
		t.Fatal("password bocor di hasil test")
	}

	noPW := connValues("baru")
	noPW.Set("password", "")
	expectStatus(t, admin.post("/admin/connections/test", noPW), http.StatusUnprocessableEntity)

	saved := testutil.SeedConnection(t, e.db)
	edit := connValues(saved.Name)
	edit.Set("password", "")
	w = admin.post(fmt.Sprintf("/admin/connections/%d/test", saved.ID), edit)
	expectStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Test koneksi berhasil.") {
		t.Fatal("edit dengan password kosong harus memakai password tersimpan")
	}
	edit.Set("port", fmt.Sprint(closedPort))
	w = admin.post(fmt.Sprintf("/admin/connections/%d/test", saved.ID), edit)
	expectStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Test koneksi gagal.") {
		t.Fatal("test edit ke port mati harus gagal")
	}
	if c, _ := e.app.Conns.Get(context.Background(), saved.ID); c.Port != 5435 {
		t.Fatal("test koneksi tidak boleh mengubah data tersimpan")
	}
	expectStatus(t, admin.post("/admin/connections/99999/test", edit), http.StatusNotFound)
	e.noSecretInLogs()
}

func TestConnectionAndAccessRoutesAreAdminOnly(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	gid := testutil.SeedGroup(t, e.db, "tim")
	alice := e.loginAs("alice", false)
	admin := e.loginAs("admin", true)
	id := conn.ID

	gets := []string{"/admin/connections", "/admin/connections/new", fmt.Sprintf("/admin/connections/%d", id), fmt.Sprintf("/admin/groups/%d/access", gid)}
	for _, p := range gets {
		expectStatus(t, alice.do("GET", p, nil), http.StatusForbidden)
	}
	posts := map[string]url.Values{
		"/admin/connections":                            connValues("ilegal"),
		"/admin/connections/test":                       connValues("ilegal"),
		fmt.Sprintf("/admin/connections/%d", id):        connValues("diubah"),
		fmt.Sprintf("/admin/connections/%d/test", id):   connValues("diubah"),
		fmt.Sprintf("/admin/connections/%d/delete", id): nil,
		fmt.Sprintf("/admin/groups/%d/access", gid):     {fmt.Sprintf("level_%d", id): {"edit"}},
	}
	for p, form := range posts {
		expectStatus(t, alice.post(p, form), http.StatusForbidden)
	}
	if e.count("SELECT count(*) FROM db_connections") != 1 || e.count("SELECT count(*) FROM db_connections WHERE name = $1", conn.Name) != 1 {
		t.Fatal("non-admin berhasil mengubah koneksi")
	}
	if e.count("SELECT count(*) FROM group_db_access") != 0 {
		t.Fatal("non-admin berhasil mengubah akses")
	}

	anon := e.client()
	for _, p := range gets {
		w := anon.do("GET", p, nil)
		expectStatus(t, w, http.StatusSeeOther)
		if w.Header().Get("Location") != "/login" {
			t.Fatalf("%s tidak diarahkan ke /login", p)
		}
	}

	expectStatus(t, admin.do("POST", fmt.Sprintf("/admin/connections/%d/delete", id), url.Values{}), http.StatusForbidden)
	expectStatus(t, admin.do("POST", "/admin/connections", connValues("tanpa-token")), http.StatusForbidden)
	expectStatus(t, admin.do("POST", fmt.Sprintf("/admin/groups/%d/access", gid), url.Values{fmt.Sprintf("level_%d", id): {"view"}}), http.StatusForbidden)
	if e.count("SELECT count(*) FROM db_connections") != 1 || e.count("SELECT count(*) FROM group_db_access") != 0 {
		t.Fatal("request tanpa CSRF mengubah data")
	}
}

func TestDeleteConnectionBlockedByHistory(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.loginAs("admin", true)
	conn := testutil.SeedConnection(t, e.db)

	t.Cleanup(func() { e.db.Exec(context.Background(), "DROP TABLE IF EXISTS zz_test_history") })
	if _, err := e.db.Exec(ctx, "CREATE TABLE zz_test_history (id serial PRIMARY KEY, db_id bigint NOT NULL REFERENCES db_connections(id))"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "INSERT INTO zz_test_history (db_id) VALUES ($1)", conn.ID); err != nil {
		t.Fatal(err)
	}
	w := admin.post(fmt.Sprintf("/admin/connections/%d/delete", conn.ID), nil)
	expectStatus(t, w, http.StatusConflict)
	if !strings.Contains(w.Body.String(), msgConnInUse) {
		t.Fatal("pesan hapus diblokir tidak ada")
	}
	if e.count("SELECT count(*) FROM db_connections WHERE id = $1", conn.ID) != 1 {
		t.Fatal("koneksi dengan riwayat terhapus")
	}
	e.db.Exec(ctx, "DELETE FROM zz_test_history")
	expectStatus(t, admin.post(fmt.Sprintf("/admin/connections/%d/delete", conn.ID), nil), http.StatusSeeOther)
}

func TestDeleteConnectionRevokesGroupSessions(t *testing.T) {
	e := setup(t)
	admin := e.loginAs("admin", true)
	conn := testutil.SeedConnection(t, e.db)
	gid := testutil.SeedGroup(t, e.db, "tim")
	alice := e.loginAs("alice", false)
	outsider := e.loginAs("bob", false)
	testutil.SeedMember(t, e.db, gid, e.userID("alice"))
	testutil.SeedGrant(t, e.db, gid, conn.ID, authz.LevelView)
	expectStatus(t, alice.do("GET", "/", nil), http.StatusOK)

	body := admin.do("GET", fmt.Sprintf("/admin/connections/%d", conn.ID), nil).Body.String()
	if !strings.Contains(body, "tim") || !strings.Contains(body, "View Only") {
		t.Fatal("halaman edit tidak menampilkan grup yang punya akses")
	}
	expectStatus(t, admin.post(fmt.Sprintf("/admin/connections/%d/delete", conn.ID), nil), http.StatusSeeOther)
	if e.count("SELECT count(*) FROM group_db_access") != 0 {
		t.Fatal("grant tidak ikut terhapus")
	}
	expectStatus(t, alice.do("GET", "/", nil), http.StatusUnauthorized)
	expectStatus(t, outsider.do("GET", "/", nil), http.StatusOK)
}

func TestGroupAccessPage(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.loginAs("admin", true)
	c1 := testutil.SeedConnection(t, e.db)
	c2 := testutil.SeedConnection(t, e.db)
	gid := testutil.SeedGroup(t, e.db, "tim")
	alice := e.loginAs("alice", false)
	testutil.SeedMember(t, e.db, gid, e.userID("alice"))
	path := fmt.Sprintf("/admin/groups/%d/access", gid)
	l1, l2 := fmt.Sprintf("level_%d", c1.ID), fmt.Sprintf("level_%d", c2.ID)

	page := admin.do("GET", path, nil)
	expectStatus(t, page, http.StatusOK)
	if !strings.Contains(page.Body.String(), c1.Name) || !strings.Contains(page.Body.String(), c2.Name) {
		t.Fatal("daftar database tidak tampil")
	}
	if !strings.Contains(admin.do("GET", fmt.Sprintf("/admin/groups/%d", gid), nil).Body.String(), path) {
		t.Fatal("halaman grup tidak punya tautan ke halaman akses")
	}
	expectStatus(t, admin.do("GET", "/admin/groups/99999/access", nil), http.StatusNotFound)

	level := func(connID int64) string {
		var l string
		if err := e.db.QueryRow(ctx, "SELECT level FROM group_db_access WHERE group_id = $1 AND db_id = $2", gid, connID).Scan(&l); err != nil {
			return "none"
		}
		return l
	}
	rotated := func(step string, want bool) {
		t.Helper()
		before := alice.session()
		expectStatus(t, alice.do("GET", "/", nil), http.StatusOK)
		if (alice.session() != before) != want {
			t.Fatalf("%s: rotasi session = %v, want %v", step, !want, want)
		}
	}

	expectStatus(t, admin.post(path, url.Values{l1: {"view"}, l2: {"edit"}}), http.StatusSeeOther)
	if level(c1.ID) != "view" || level(c2.ID) != "edit" {
		t.Fatalf("level salah: %s %s", level(c1.ID), level(c2.ID))
	}
	rotated("akses naik", true)
	page = admin.do("GET", path+"?ok=access_updated", nil)
	if !strings.Contains(page.Body.String(), "Akses database grup berhasil diperbarui.") || !strings.Contains(page.Body.String(), `<option value="edit" selected>`) {
		t.Fatal("halaman tidak mencerminkan level tersimpan")
	}

	expectStatus(t, admin.post(path, url.Values{l1: {"view"}, l2: {"edit"}}), http.StatusSeeOther)
	rotated("tanpa perubahan", false)

	w := admin.post(path, url.Values{l1: {"edit"}, l2: {"superadmin"}})
	expectStatus(t, w, http.StatusUnprocessableEntity)
	if !strings.Contains(w.Body.String(), "Level akses tidak valid.") || level(c1.ID) != "view" || level(c2.ID) != "edit" {
		t.Fatal("form tidak valid harus ditolak seluruhnya")
	}
	rotated("form ditolak", false)

	expectStatus(t, admin.post(path, url.Values{l1: {"edit"}}), http.StatusSeeOther)
	if level(c1.ID) != "edit" || level(c2.ID) != "edit" {
		t.Fatal("field yang tidak dikirim ikut berubah")
	}
	rotated("view -> edit", true)

	expectStatus(t, admin.post(path, url.Values{l1: {"view"}}), http.StatusSeeOther)
	expectStatus(t, alice.do("GET", "/", nil), http.StatusUnauthorized)
	expectStatus(t, alice.login("alice", "password123"), http.StatusSeeOther)

	expectStatus(t, admin.post(path, url.Values{l1: {"none"}, l2: {"none"}}), http.StatusSeeOther)
	if e.count("SELECT count(*) FROM group_db_access") != 0 {
		t.Fatal("grant tidak dicabut")
	}
	expectStatus(t, alice.do("GET", "/", nil), http.StatusUnauthorized)
}
