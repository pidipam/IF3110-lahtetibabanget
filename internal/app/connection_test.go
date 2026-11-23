package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

const secretPW = "pw-rahasia-Zq9x7"

const closedPort = 5499

const msgConnInUse = "Koneksi tidak dapat dihapus karena sudah memiliki riwayat migration."

type connBody struct {
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"db_name"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
}

type connResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Engine    string `json:"engine"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	DBName    string `json:"db_name"`
	Username  string `json:"username"`
	SSLMode   string `json:"sslmode"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type connListResponse struct {
	Items []connResponse `json:"items"`
}

type testConnResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func connInput(name string) connBody {
	return connBody{Name: name, Engine: dbconn.EnginePostgres, Host: "127.0.0.1", Port: 5435, DBName: "sample", Username: "app", Password: secretPW, SSLMode: "disable"}
}

func (e *testEnv) loginAs(username string, admin bool) *client {
	e.t.Helper()
	e.createUser(username, "password123", admin)
	c := e.client()
	expectStatus(e.t, c.login(username, "password123"), http.StatusSeeOther)
	return c
}

func (e *testEnv) apiUser(username string, admin bool) *apiClient {
	e.t.Helper()
	e.createUser(username, "password123", admin)
	return e.apiLogin(username, "password123")
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
	admin := e.apiUser("admin", true)

	w := admin.json("POST", "/api/admin/connections", connInput("sampel"))
	expectStatus(t, w, http.StatusCreated)
	var created connResponse
	decodeBody(t, w, &created)
	if created.ID == 0 || created.Name != "sampel" || created.Host != "127.0.0.1" || created.Port != 5435 {
		t.Fatalf("respons create: %+v", created)
	}
	var enc string
	if err := e.db.QueryRow(ctx, "SELECT password_enc FROM db_connections WHERE id = $1", created.ID).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	if enc == "" || strings.Contains(enc, secretPW) {
		t.Fatalf("password harus tersimpan terenkripsi, bukan plaintext: %q", enc)
	}
	if strings.Contains(w.Body.String(), secretPW) || strings.Contains(w.Body.String(), enc) {
		t.Fatal("password atau ciphertext bocor ke respons")
	}

	w = admin.json("GET", "/api/admin/connections", nil)
	expectStatus(t, w, http.StatusOK)
	var list connListResponse
	decodeBody(t, w, &list)
	if len(list.Items) != 1 || list.Items[0].Name != "sampel" {
		t.Fatalf("daftar koneksi: %+v", list)
	}
	if strings.Contains(w.Body.String(), secretPW) || strings.Contains(w.Body.String(), enc) {
		t.Fatal("password atau ciphertext bocor ke daftar")
	}

	w = admin.json("GET", fmt.Sprintf("/api/admin/connections/%d", created.ID), nil)
	expectStatus(t, w, http.StatusOK)
	var got connResponse
	decodeBody(t, w, &got)
	if got.ID != created.ID || got.Name != "sampel" {
		t.Fatalf("detail koneksi: %+v", got)
	}
	if strings.Contains(w.Body.String(), secretPW) || strings.Contains(w.Body.String(), enc) {
		t.Fatal("password atau ciphertext bocor ke detail")
	}

	update := connInput("sampel-baru")
	update.Password = ""
	w = admin.json("PATCH", fmt.Sprintf("/api/admin/connections/%d", created.ID), update)
	expectStatus(t, w, http.StatusOK)
	creds, err := e.app.Conns.Credentials(ctx, created.ID)
	if err != nil || creds.Password != secretPW {
		t.Fatalf("password kosong harus mempertahankan password lama: %v", err)
	}
	if e.count("SELECT count(*) FROM db_connections WHERE name = $1", "sampel-baru") != 1 {
		t.Fatal("nama koneksi tidak berubah")
	}

	update.Password = "password-baru"
	expectStatus(t, admin.json("PATCH", fmt.Sprintf("/api/admin/connections/%d", created.ID), update), http.StatusOK)
	if creds, _ := e.app.Conns.Credentials(ctx, created.ID); creds.Password != "password-baru" {
		t.Fatal("password baru tidak tersimpan")
	}

	expectStatus(t, admin.json("DELETE", fmt.Sprintf("/api/admin/connections/%d", created.ID), nil), http.StatusNoContent)
	if e.count("SELECT count(*) FROM db_connections") != 0 {
		t.Fatal("koneksi tidak terhapus")
	}
	expectAPIError(t, admin.json("GET", fmt.Sprintf("/api/admin/connections/%d", created.ID), nil), http.StatusNotFound, "not_found")
	expectAPIError(t, admin.json("DELETE", fmt.Sprintf("/api/admin/connections/%d", created.ID), nil), http.StatusNotFound, "not_found")
	expectAPIError(t, admin.json("PATCH", "/api/admin/connections/abc", connInput("x")), http.StatusNotFound, "not_found")
	e.noSecretInLogs()
}

func TestAdminConnectionValidation(t *testing.T) {
	e := setup(t)
	admin := e.apiUser("admin", true)
	type validationCase struct {
		name  string
		body  connBody
		field string
	}
	base := connInput("valid")
	with := func(fn func(*connBody)) connBody {
		b := base
		fn(&b)
		return b
	}
	cases := []validationCase{
		{"nama kosong", with(func(b *connBody) { b.Name = "  " }), "name"},
		{"nama terlalu panjang", with(func(b *connBody) { b.Name = strings.Repeat("a", 65) }), "name"},
		{"host kosong", with(func(b *connBody) { b.Host = "" }), "host"},
		{"host berisi simbol", with(func(b *connBody) { b.Host = "a/b@c" }), "host"},
		{"port nol", with(func(b *connBody) { b.Port = 0 }), "port"},
		{"port terlalu besar", with(func(b *connBody) { b.Port = 70000 }), "port"},
		{"database kosong", with(func(b *connBody) { b.DBName = "" }), "db_name"},
		{"username kosong", with(func(b *connBody) { b.Username = "" }), "username"},
		{"password kosong", with(func(b *connBody) { b.Password = "" }), "password"},
		{"sslmode salah", with(func(b *connBody) { b.SSLMode = "bebas" }), "sslmode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := admin.json("POST", "/api/admin/connections", tc.body)
			expectInvalidField(t, w, tc.field)
			if strings.Contains(w.Body.String(), secretPW) {
				t.Fatal("password diisi ulang ke respons")
			}
		})
	}
	if e.count("SELECT count(*) FROM db_connections") != 0 {
		t.Fatal("koneksi tidak valid tersimpan")
	}

	expectStatus(t, admin.json("POST", "/api/admin/connections", connInput("kembar")), http.StatusCreated)
	expectAPIError(t, admin.json("POST", "/api/admin/connections", connInput("kembar")), http.StatusConflict, "conflict")
	other := testutil.SeedConnection(t, e.db)
	expectAPIError(t, admin.json("PATCH", fmt.Sprintf("/api/admin/connections/%d", other.ID), connInput("kembar")), http.StatusConflict, "conflict")
	if e.count("SELECT count(*) FROM db_connections") != 2 {
		t.Fatal("jumlah koneksi berubah")
	}
	e.noSecretInLogs()
}

func TestAdminConnectionTestButton(t *testing.T) {
	e := setup(t)
	admin := e.apiUser("admin", true)

	valid := connInput("baru")
	valid.Password = "app"
	w := admin.json("POST", "/api/admin/connections/test", valid)
	expectStatus(t, w, http.StatusOK)
	var result testConnResponse
	decodeBody(t, w, &result)
	if !result.Success || result.Message == "" {
		t.Fatalf("test koneksi benar: %+v", result)
	}
	if e.count("SELECT count(*) FROM db_connections") != 0 {
		t.Fatal("test koneksi tidak boleh menyimpan apa pun")
	}

	bad := valid
	bad.Port = closedPort
	w = admin.json("POST", "/api/admin/connections/test", bad)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &result)
	if result.Success || result.Code != dbconn.KindRefused.String() || !strings.Contains(result.Message, dbconn.KindRefused.UserMessage()) {
		t.Fatalf("test koneksi gagal: %+v", result)
	}
	if strings.Contains(w.Body.String(), secretPW) || strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Fatal("detail koneksi bocor di hasil test")
	}

	noPW := valid
	noPW.Password = ""
	expectAPIError(t, admin.json("POST", "/api/admin/connections/test", noPW), http.StatusUnprocessableEntity, "validation_error")

	saved := testutil.SeedConnection(t, e.db)
	edit := connInput(saved.Name)
	edit.Password = ""
	w = admin.json("POST", fmt.Sprintf("/api/admin/connections/%d/test", saved.ID), edit)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &result)
	if !result.Success {
		t.Fatalf("edit dengan password kosong harus memakai password tersimpan: %+v", result)
	}
	edit.Port = closedPort
	w = admin.json("POST", fmt.Sprintf("/api/admin/connections/%d/test", saved.ID), edit)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &result)
	if result.Success {
		t.Fatal("test edit ke port mati harus gagal")
	}
	if c, _ := e.app.Conns.Get(context.Background(), saved.ID); c.Port != 5435 {
		t.Fatal("test koneksi tidak boleh mengubah data tersimpan")
	}
	expectAPIError(t, admin.json("POST", "/api/admin/connections/99999/test", edit), http.StatusNotFound, "not_found")
	e.noSecretInLogs()
}

func TestConnectionAPIAdminOnly(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	admin := e.apiUser("admin", true)
	alice := e.apiUser("alice", false)
	id := conn.ID

	gets := []string{"/api/admin/connections", fmt.Sprintf("/api/admin/connections/%d", id)}
	for _, p := range gets {
		expectAPIError(t, alice.json("GET", p, nil), http.StatusForbidden, "forbidden")
		expectAPIError(t, e.api().json("GET", p, nil), http.StatusUnauthorized, "unauthenticated")
	}
	posts := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/admin/connections", connInput("ilegal")},
		{"POST", "/api/admin/connections/test", connInput("ilegal")},
		{"PATCH", fmt.Sprintf("/api/admin/connections/%d", id), connInput("diubah")},
		{"POST", fmt.Sprintf("/api/admin/connections/%d/test", id), connInput("diubah")},
		{"DELETE", fmt.Sprintf("/api/admin/connections/%d", id), nil},
	}
	for _, tc := range posts {
		expectAPIError(t, alice.json(tc.method, tc.path, tc.body), http.StatusForbidden, "forbidden")
	}
	if e.count("SELECT count(*) FROM db_connections") != 1 || e.count("SELECT count(*) FROM db_connections WHERE name = $1", conn.Name) != 1 {
		t.Fatal("non-admin berhasil mengubah koneksi")
	}

	body, err := json.Marshal(connInput("tanpa-token"))
	if err != nil {
		t.Fatal(err)
	}
	expectAPIError(t, admin.send("POST", "/api/admin/connections", string(body), ""), http.StatusForbidden, "csrf_invalid")
	if e.count("SELECT count(*) FROM db_connections") != 1 {
		t.Fatal("request tanpa CSRF mengubah data")
	}
}

func TestDeleteConnectionBlockedByHistory(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.apiUser("admin", true)
	conn := testutil.SeedConnection(t, e.db)

	t.Cleanup(func() { e.db.Exec(context.Background(), "DROP TABLE IF EXISTS zz_test_history") })
	if _, err := e.db.Exec(ctx, "CREATE TABLE zz_test_history (id serial PRIMARY KEY, db_id bigint NOT NULL REFERENCES db_connections(id))"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "INSERT INTO zz_test_history (db_id) VALUES ($1)", conn.ID); err != nil {
		t.Fatal(err)
	}
	w := admin.json("DELETE", fmt.Sprintf("/api/admin/connections/%d", conn.ID), nil)
	expectAPIError(t, w, http.StatusConflict, "conflict")
	if !strings.Contains(w.Body.String(), msgConnInUse) {
		t.Fatal("pesan hapus diblokir tidak ada")
	}
	if e.count("SELECT count(*) FROM db_connections WHERE id = $1", conn.ID) != 1 {
		t.Fatal("koneksi dengan riwayat terhapus")
	}
	e.db.Exec(ctx, "DELETE FROM zz_test_history")
	expectStatus(t, admin.json("DELETE", fmt.Sprintf("/api/admin/connections/%d", conn.ID), nil), http.StatusNoContent)
}

func TestDeleteConnectionRevokesGroupSessions(t *testing.T) {
	e := setup(t)
	admin := e.apiUser("admin", true)
	conn := testutil.SeedConnection(t, e.db)
	gid := testutil.SeedGroup(t, e.db, "tim")
	alice := e.apiUser("alice", false)
	outsider := e.apiUser("bob", false)
	testutil.SeedMember(t, e.db, gid, e.userID("alice"))
	testutil.SeedGrant(t, e.db, gid, conn.ID, authz.LevelView)
	expectStatus(t, alice.json("GET", "/api/me", nil), http.StatusOK)

	expectStatus(t, admin.json("DELETE", fmt.Sprintf("/api/admin/connections/%d", conn.ID), nil), http.StatusNoContent)
	if e.count("SELECT count(*) FROM group_db_access") != 0 {
		t.Fatal("grant tidak ikut terhapus")
	}
	expectAPIError(t, alice.json("GET", "/api/me", nil), http.StatusUnauthorized, "session_expired")
	expectStatus(t, outsider.json("GET", "/api/me", nil), http.StatusOK)
}
