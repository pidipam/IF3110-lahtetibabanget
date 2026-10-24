package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

var testSession = auth.SessionConfig{IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 8 * time.Hour, RotateInterval: 15 * time.Minute, GracePeriod: 30 * time.Second}

type testEnv struct {
	t   *testing.T
	app *App
	db  *pgxpool.Pool
	now time.Time
	log *bytes.Buffer
}

func setup(t *testing.T) *testEnv {
	return setupWith(t, false)
}

func setupWith(t *testing.T, secure bool) *testEnv {
	t.Helper()
	db := testutil.NewTestDB(t)
	env := &testEnv{t: t, db: db, now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), log: &bytes.Buffer{}}
	a, err := New(Options{DB: db, Session: testSession, Secure: secure, Logger: slog.New(slog.NewTextHandler(env.log, nil)), EncryptionKey: testutil.TestKey})
	if err != nil {
		t.Fatal(err)
	}
	a.Sessions.Now = func() time.Time { return env.now }
	env.app = a
	return env
}

func (e *testEnv) advance(d time.Duration) {
	e.now = e.now.Add(d)
}

func (e *testEnv) createUser(username, password string, admin bool) *users.User {
	e.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		e.t.Fatal(err)
	}
	u, err := e.app.Users.Create(context.Background(), username, hash, admin)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *testEnv) count(query string, args ...any) int {
	e.t.Helper()
	return testutil.Count(e.t, e.db, query, args...)
}

type client struct {
	c       *testutil.Client
	cookies map[string]string
}

func (e *testEnv) client() *client {
	c := testutil.NewClient(e.t, e.app)
	return &client{c: c, cookies: c.Cookies}
}

func (c *client) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	return c.c.Do(method, path, form)
}

func (c *client) post(path string, form url.Values) *httptest.ResponseRecorder {
	return c.c.Post(path, form)
}

func (c *client) login(username, password string) *httptest.ResponseRecorder {
	return c.c.Login(username, password)
}

func (c *client) session() string {
	return c.c.Session()
}

func expectStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	testutil.ExpectStatus(t, w, want)
}

func sessionSetCookie(w *httptest.ResponseRecorder) string {
	return testutil.SessionSetCookie(w)
}

func TestLoginSuccessSetsSecureSessionCookie(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.do("GET", "/login", nil)
	w := c.post("/login", url.Values{"username": {"alice"}, "password": {"password123"}})
	expectStatus(t, w, http.StatusSeeOther)
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Fatalf("redirect ke %q", loc)
	}
	header := sessionSetCookie(w)
	if !strings.Contains(header, "HttpOnly") || !strings.Contains(header, "SameSite=Lax") {
		t.Fatalf("atribut cookie kurang: %s", header)
	}
	if strings.Contains(header, "Secure") {
		t.Fatalf("development tidak memakai Secure: %s", header)
	}
	token := c.session()
	if token == "" {
		t.Fatal("cookie session tidak diset")
	}
	home := c.do("GET", "/", nil)
	expectStatus(t, home, http.StatusOK)
	if !strings.Contains(home.Body.String(), "alice") {
		t.Fatal("beranda tidak menampilkan user")
	}
	if strings.Contains(home.Body.String(), token) || strings.Contains(home.Body.String(), "password123") {
		t.Fatal("response memuat session id atau password")
	}
	if e.count("SELECT count(*) FROM sessions WHERE token_hash = $1", []byte(token)) != 0 {
		t.Fatal("token disimpan mentah di database")
	}
	if e.count("SELECT count(*) FROM sessions WHERE token_hash = $1", auth.HashToken(token)) != 1 {
		t.Fatal("hash token tidak tersimpan")
	}
}

func TestSecureCookieInProduction(t *testing.T) {
	e := setupWith(t, true)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.do("GET", "/login", nil)
	w := c.post("/login", url.Values{"username": {"alice"}, "password": {"password123"}})
	expectStatus(t, w, http.StatusSeeOther)
	if !strings.Contains(sessionSetCookie(w), "Secure") {
		t.Fatal("production harus memakai cookie Secure")
	}
}

func TestLoginFailures(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	nonaktif := e.createUser("budi", "password123", false)
	e.app.Users.Update(context.Background(), nonaktif.ID, "budi", false, false)
	cases := []struct{ name, username, password string }{{"password salah", "alice", "salahsalah"}, {"user tidak ada", "siapa", "password123"}, {"user nonaktif", "budi", "password123"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := e.client()
			w := c.login(tc.username, tc.password)
			expectStatus(t, w, http.StatusUnauthorized)
			if !strings.Contains(w.Body.String(), "Username atau password salah.") {
				t.Fatal("pesan error harus generik")
			}
			if c.session() != "" {
				t.Fatal("login gagal tidak boleh membuat session")
			}
		})
	}
	if n := e.count("SELECT count(*) FROM sessions"); n != 0 {
		t.Fatalf("ada %d session padahal semua login gagal", n)
	}
}

func TestLoginRequiresCSRF(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.do("GET", "/login", nil)
	w := c.do("POST", "/login", url.Values{"username": {"alice"}, "password": {"password123"}})
	expectStatus(t, w, http.StatusForbidden)
	w = c.do("POST", "/login", url.Values{"username": {"alice"}, "password": {"password123"}, auth.CSRFField: {"palsu"}})
	expectStatus(t, w, http.StatusForbidden)
	if c.session() != "" {
		t.Fatal("login tanpa CSRF tidak boleh berhasil")
	}
}

func TestAnonymousIsRedirectedToLogin(t *testing.T) {
	e := setup(t)
	c := e.client()
	for _, path := range []string{"/", "/admin/users", "/admin/groups"} {
		w := c.do("GET", path, nil)
		expectStatus(t, w, http.StatusSeeOther)
		if w.Header().Get("Location") != "/login" {
			t.Fatalf("%s tidak diarahkan ke /login", path)
		}
	}
}

func TestIdleTimeout(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.login("alice", "password123")
	e.advance(20 * time.Minute)
	expectStatus(t, c.do("GET", "/", nil), http.StatusOK)
	e.advance(20 * time.Minute)
	expectStatus(t, c.do("GET", "/", nil), http.StatusOK)
	e.advance(31 * time.Minute)
	w := c.do("GET", "/", nil)
	expectStatus(t, w, http.StatusUnauthorized)
	if !strings.Contains(w.Body.String(), "Sesi Berakhir") {
		t.Fatal("halaman session expired tidak muncul")
	}
	if c.session() != "" {
		t.Fatal("cookie session harus dihapus")
	}
}

func TestAbsoluteExpirationNotExtendedByRotation(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.login("alice", "password123")
	var before time.Time
	e.db.QueryRow(context.Background(), "SELECT expires_at FROM sessions").Scan(&before)
	rotations := 0
	for elapsed := 10 * time.Minute; elapsed < 8*time.Hour; elapsed += 10 * time.Minute {
		e.advance(10 * time.Minute)
		old := c.session()
		expectStatus(t, c.do("GET", "/", nil), http.StatusOK)
		if c.session() != old {
			rotations++
		}
	}
	if rotations == 0 {
		t.Fatal("tidak ada rotasi berkala")
	}
	var after time.Time
	e.db.QueryRow(context.Background(), "SELECT expires_at FROM sessions").Scan(&after)
	if !after.Equal(before) {
		t.Fatalf("rotasi memperpanjang absolute expiration: %v -> %v", before, after)
	}
	e.advance(10 * time.Minute)
	expectStatus(t, c.do("GET", "/", nil), http.StatusUnauthorized)
}

func TestPeriodicRotationWithGracePeriod(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.login("alice", "password123")
	oldToken := c.session()
	e.advance(16 * time.Minute)
	w := c.do("GET", "/", nil)
	expectStatus(t, w, http.StatusOK)
	newToken := c.session()
	if newToken == "" || newToken == oldToken {
		t.Fatal("session id tidak dirotasi")
	}
	stale := e.client()
	stale.cookies[auth.SessionCookie] = oldToken
	e.advance(10 * time.Second)
	w = stale.do("GET", "/", nil)
	expectStatus(t, w, http.StatusOK)
	if sessionSetCookie(w) != "" {
		t.Fatal("identifier lama dalam grace period tidak boleh memicu rotasi baru")
	}
	e.advance(25 * time.Second)
	expectStatus(t, stale.do("GET", "/", nil), http.StatusUnauthorized)
	expectStatus(t, c.do("GET", "/", nil), http.StatusOK)
}

func TestLoginRotatesSession(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.login("alice", "password123")
	first := c.session()
	w := c.post("/login", url.Values{"username": {"alice"}, "password": {"password123"}})
	expectStatus(t, w, http.StatusSeeOther)
	if c.session() == first {
		t.Fatal("login ulang harus menghasilkan session id baru")
	}
	old := e.client()
	old.cookies[auth.SessionCookie] = first
	expectStatus(t, old.do("GET", "/", nil), http.StatusUnauthorized)
}

func TestPrivilegeEscalationRotatesSession(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	bob := e.createUser("bob", "password123", false)
	admin := e.client()
	admin.login("admin", "password123")
	b := e.client()
	b.login("bob", "password123")
	before := b.session()
	expectStatus(t, admin.post(fmt.Sprintf("/admin/users/%d", bob.ID), url.Values{"username": {"bob"}, "is_admin": {"on"}, "is_active": {"on"}}), http.StatusSeeOther)
	expectStatus(t, b.do("GET", "/", nil), http.StatusOK)
	if b.session() == before {
		t.Fatal("session tidak dirotasi setelah privilege naik")
	}
	expectStatus(t, b.do("GET", "/admin/users", nil), http.StatusOK)
	g, _ := e.app.Users.CreateGroup(context.Background(), "analis", "")
	before = b.session()
	expectStatus(t, admin.post(fmt.Sprintf("/admin/groups/%d/members", g.ID), url.Values{"user_id": {fmt.Sprint(bob.ID)}}), http.StatusSeeOther)
	expectStatus(t, b.do("GET", "/", nil), http.StatusOK)
	if b.session() == before {
		t.Fatal("session tidak dirotasi setelah masuk group")
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.login("alice", "password123")
	token := c.session()
	w := c.post("/logout", nil)
	expectStatus(t, w, http.StatusSeeOther)
	if c.session() != "" {
		t.Fatal("cookie session tidak dihapus saat logout")
	}
	if e.count("SELECT count(*) FROM sessions WHERE revoked_at IS NOT NULL") != 1 {
		t.Fatal("session tidak dicabut di backend")
	}
	replay := e.client()
	replay.cookies[auth.SessionCookie] = token
	expectStatus(t, replay.do("GET", "/", nil), http.StatusUnauthorized)
}

func TestLogoutRequiresCSRF(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.login("alice", "password123")
	expectStatus(t, c.do("POST", "/logout", url.Values{}), http.StatusForbidden)
	expectStatus(t, c.do("GET", "/", nil), http.StatusOK)
}

func TestStateChangeWithoutCSRFRejected(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	c := e.client()
	c.login("admin", "password123")
	expectStatus(t, c.do("POST", "/admin/groups", url.Values{"name": {"tanpa-token"}}), http.StatusForbidden)
	expectStatus(t, c.do("POST", "/admin/groups", url.Values{"name": {"token-salah"}, auth.CSRFField: {"x"}}), http.StatusForbidden)
	if e.count("SELECT count(*) FROM groups") != 0 {
		t.Fatal("group dibuat tanpa CSRF valid")
	}
	expectStatus(t, c.post("/admin/groups", url.Values{"name": {"dengan-token"}}), http.StatusSeeOther)
	if e.count("SELECT count(*) FROM groups") != 1 {
		t.Fatal("group dengan CSRF valid gagal dibuat")
	}
}

func TestNonAdminForbidden(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	target := e.createUser("budi", "password123", false)
	c := e.client()
	c.login("alice", "password123")
	for _, path := range []string{"/admin/users", "/admin/users/new", "/admin/groups", fmt.Sprintf("/admin/users/%d", target.ID)} {
		expectStatus(t, c.do("GET", path, nil), http.StatusForbidden)
	}
	expectStatus(t, c.post("/admin/groups", url.Values{"name": {"ilegal"}}), http.StatusForbidden)
	expectStatus(t, c.post("/admin/users", url.Values{"username": {"ilegal"}, "password": {"password123"}, "is_admin": {"on"}}), http.StatusForbidden)
	expectStatus(t, c.post(fmt.Sprintf("/admin/users/%d", target.ID), url.Values{"username": {"budi"}, "is_admin": {"on"}, "is_active": {"on"}}), http.StatusForbidden)
	if e.count("SELECT count(*) FROM groups") != 0 || e.count("SELECT count(*) FROM users WHERE is_admin") != 0 {
		t.Fatal("non-admin berhasil mengubah data")
	}
}

func TestAdminManagesUsersAndGroups(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	c := e.client()
	c.login("admin", "password123")
	expectStatus(t, c.post("/admin/users", url.Values{"username": {"Carol"}, "password": {"password123"}}), http.StatusSeeOther)
	expectStatus(t, c.post("/admin/users", url.Values{"username": {"carol"}, "password": {"password123"}}), http.StatusUnprocessableEntity)
	expectStatus(t, c.post("/admin/users", url.Values{"username": {"x"}, "password": {"password123"}}), http.StatusUnprocessableEntity)
	expectStatus(t, c.post("/admin/users", url.Values{"username": {"dodi"}, "password": {"pendek"}}), http.StatusUnprocessableEntity)
	carol, err := e.app.Users.GetByUsername(context.Background(), "carol")
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, c.post("/admin/groups", url.Values{"name": {"tim-data"}, "description": {"Analis"}}), http.StatusSeeOther)
	expectStatus(t, c.post("/admin/groups", url.Values{"name": {"tim-data"}}), http.StatusUnprocessableEntity)
	var groupID int64
	e.db.QueryRow(context.Background(), "SELECT id FROM groups WHERE name = 'tim-data'").Scan(&groupID)
	expectStatus(t, c.post(fmt.Sprintf("/admin/groups/%d/members", groupID), url.Values{"user_id": {fmt.Sprint(carol.ID)}}), http.StatusSeeOther)
	page := c.do("GET", fmt.Sprintf("/admin/groups/%d", groupID), nil)
	expectStatus(t, page, http.StatusOK)
	if !strings.Contains(page.Body.String(), "carol") {
		t.Fatal("anggota tidak tampil")
	}
	user := e.client()
	expectStatus(t, user.login("carol", "password123"), http.StatusSeeOther)
	expectStatus(t, c.post(fmt.Sprintf("/admin/groups/%d/members/%d/delete", groupID, carol.ID), nil), http.StatusSeeOther)
	if e.count("SELECT count(*) FROM group_members") != 0 {
		t.Fatal("anggota tidak dikeluarkan")
	}
	expectStatus(t, user.do("GET", "/", nil), http.StatusUnauthorized)
	expectStatus(t, c.post(fmt.Sprintf("/admin/groups/%d/delete", groupID), nil), http.StatusSeeOther)
	if e.count("SELECT count(*) FROM groups") != 0 {
		t.Fatal("group tidak terhapus")
	}
}

func TestDeactivateRevokesSessions(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	bob := e.createUser("bob", "password123", false)
	admin := e.client()
	admin.login("admin", "password123")
	b := e.client()
	b.login("bob", "password123")
	expectStatus(t, admin.post(fmt.Sprintf("/admin/users/%d", bob.ID), url.Values{"username": {"bob"}}), http.StatusSeeOther)
	expectStatus(t, b.do("GET", "/", nil), http.StatusUnauthorized)
	expectStatus(t, e.client().login("bob", "password123"), http.StatusUnauthorized)
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	bob := e.createUser("bob", "password123", false)
	admin := e.client()
	admin.login("admin", "password123")
	b := e.client()
	b.login("bob", "password123")
	expectStatus(t, admin.post(fmt.Sprintf("/admin/users/%d/password", bob.ID), url.Values{"password": {"passwordbaru"}}), http.StatusSeeOther)
	expectStatus(t, b.do("GET", "/", nil), http.StatusUnauthorized)
	expectStatus(t, e.client().login("bob", "password123"), http.StatusUnauthorized)
	expectStatus(t, e.client().login("bob", "passwordbaru"), http.StatusSeeOther)
}

func TestAdminCannotLockThemselvesOut(t *testing.T) {
	e := setup(t)
	me := e.createUser("admin", "password123", true)
	c := e.client()
	c.login("admin", "password123")
	expectStatus(t, c.post(fmt.Sprintf("/admin/users/%d", me.ID), url.Values{"username": {"admin"}, "is_active": {"on"}}), http.StatusUnprocessableEntity)
	if e.count("SELECT count(*) FROM users WHERE is_admin AND is_active") != 1 {
		t.Fatal("admin berhasil mencabut hak dirinya sendiri")
	}
}

func TestEffectiveAccessFromDatabase(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	alice := e.createUser("alice", "password123", false)
	viewers, _ := e.app.Users.CreateGroup(ctx, "viewers", "")
	editors, _ := e.app.Users.CreateGroup(ctx, "editors", "")
	e.app.Users.AddMember(ctx, viewers.ID, alice.ID)
	e.app.Users.AddMember(ctx, editors.ID, alice.ID)
	db1 := testutil.SeedConnection(t, e.db).ID
	db2 := testutil.SeedConnection(t, e.db).ID
	db3 := testutil.SeedConnection(t, e.db).ID
	if _, err := e.db.Exec(ctx, "INSERT INTO group_db_access (group_id, db_id, level) VALUES ($1, $3, 'view'), ($2, $3, 'edit'), ($1, $4, 'view')", viewers.ID, editors.ID, db1, db2); err != nil {
		t.Fatal(err)
	}
	p := authz.Principal{UserID: alice.ID}
	if err := e.app.Authz.Can(ctx, p, db1, authz.EditMigration); err != nil {
		t.Fatalf("db 1 harus View + Edit (level tertinggi): %v", err)
	}
	if err := e.app.Authz.Can(ctx, p, db2, authz.ViewDatabase); err != nil {
		t.Fatalf("db 2 harus bisa dilihat: %v", err)
	}
	if err := e.app.Authz.Can(ctx, p, db2, authz.EditMigration); err != authz.ErrForbidden {
		t.Fatalf("db 2 hanya View Only, got %v", err)
	}
	if err := e.app.Authz.Can(ctx, p, db3, authz.ViewDatabase); err != authz.ErrForbidden {
		t.Fatalf("db 3 tanpa akses, got %v", err)
	}
	dbs, err := e.app.Authz.AccessibleDatabases(ctx, p)
	if err != nil || len(dbs) != 2 || dbs[db1] != authz.LevelEdit {
		t.Fatalf("AccessibleDatabases salah: %v %v", dbs, err)
	}
	e.app.Users.Update(ctx, alice.ID, "alice", false, false)
	if err := e.app.Authz.Can(ctx, p, db1, authz.ViewDatabase); err != authz.ErrForbidden {
		t.Fatalf("user nonaktif tidak boleh punya akses, got %v", err)
	}
}

func TestSecretsNeverLogged(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.client()
	c.login("alice", "rahasia-salah-xyz")
	c.login("alice", "password123")
	token := c.session()
	c.do("GET", "/?q=1", nil)
	c.post("/logout", nil)
	logs := e.log.String()
	for _, secret := range []string{"password123", "rahasia-salah-xyz", token} {
		if strings.Contains(logs, secret) {
			t.Fatalf("log memuat data rahasia %q", secret)
		}
	}
}

func TestSeedAdmin(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := SeedAdmin(ctx, e.app.Users, "admin", ""); err == nil {
		t.Fatal("tanpa password dan tanpa admin harus error")
	}
	created, err := SeedAdmin(ctx, e.app.Users, "admin", "password123")
	if err != nil || !created {
		t.Fatalf("admin pertama gagal dibuat: %v", err)
	}
	created, err = SeedAdmin(ctx, e.app.Users, "admin", "password123")
	if err != nil || created {
		t.Fatalf("seed kedua harus no-op: %v", err)
	}
	expectStatus(t, e.client().login("admin", "password123"), http.StatusSeeOther)
}
