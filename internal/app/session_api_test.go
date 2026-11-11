package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/sessionapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func TestAPILoginSuccess(t *testing.T) {
	e := setup(t)
	alice := e.createUser("alice", "password123", false)
	a := testutil.SeedConnection(t, e.db)
	b := testutil.SeedConnection(t, e.db)
	testutil.SeedConnection(t, e.db)
	viewers := testutil.SeedGroup(t, e.db, "viewers")
	editors := testutil.SeedGroup(t, e.db, "editors")
	testutil.SeedMember(t, e.db, viewers, alice.ID)
	testutil.SeedMember(t, e.db, editors, alice.ID)
	testutil.SeedGrant(t, e.db, viewers, a.ID, authz.LevelView)
	testutil.SeedGrant(t, e.db, viewers, b.ID, authz.LevelView)
	testutil.SeedGrant(t, e.db, editors, b.ID, authz.LevelEdit)
	c := e.api()
	c.fetchCSRF()
	preCSRF := c.csrf
	w := c.login("  Alice ", "password123")
	expectStatus(t, w, http.StatusOK)
	header := sessionSetCookie(w)
	if !strings.Contains(header, "HttpOnly") || !strings.Contains(header, "SameSite=Lax") {
		t.Fatalf("atribut cookie kurang: %s", header)
	}
	token := c.cookies[auth.SessionCookie]
	if token == "" {
		t.Fatal("cookie session tidak diset")
	}
	if strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), "password123") {
		t.Fatal("response memuat session id atau password")
	}
	if c.csrf == "" || c.csrf == preCSRF {
		t.Fatal("login harus mengembalikan token CSRF sesi yang baru")
	}
	me := c.send("GET", "/api/me", "", "")
	expectStatus(t, me, http.StatusOK)
	var out sessionapi.MeResponse
	decodeBody(t, me, &out)
	if out.User.ID != alice.ID || out.User.Username != "alice" || out.IsAdmin || out.CSRFToken != c.csrf {
		t.Fatalf("isi /api/me salah: %+v", out)
	}
	levels := map[int64]string{}
	for _, d := range out.Databases {
		levels[d.ID] = d.Level
		if d.Name == "" || d.Engine == "" {
			t.Fatalf("database tanpa nama atau engine: %+v", d)
		}
	}
	if len(levels) != 2 || levels[a.ID] != "view" || levels[b.ID] != "edit" {
		t.Fatalf("level per database salah: %+v", levels)
	}
	var csrf sessionapi.CSRFResponse
	decodeBody(t, c.send("GET", "/api/csrf", "", ""), &csrf)
	if csrf.CSRFToken != c.csrf {
		t.Fatal("/api/csrf setelah login harus memberi token sesi")
	}
}

func TestAPIMeAdminWithoutDatabases(t *testing.T) {
	e := setup(t)
	e.createUser("root", "password123", true)
	c := e.api()
	expectStatus(t, c.login("root", "password123"), http.StatusOK)
	w := c.send("GET", "/api/me", "", "")
	expectStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), `"databases":[]`) || !strings.Contains(w.Body.String(), `"is_admin":true`) {
		t.Fatalf("admin tanpa grant harus mendapat daftar database kosong: %s", w.Body.String())
	}
}

func TestAPILoginFailures(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	nonaktif := e.createUser("budi", "password123", false)
	e.app.Users.Update(context.Background(), nonaktif.ID, "budi", false, false)
	cases := []struct{ name, username, password string }{{"password salah", "alice", "salahsalah"}, {"user tidak ada", "siapa", "password123"}, {"user nonaktif", "budi", "password123"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := e.api()
			w := c.login(tc.username, tc.password)
			expectAPIError(t, w, http.StatusUnauthorized, "invalid_credentials")
			if !strings.Contains(w.Body.String(), "Username atau password salah.") {
				t.Fatal("pesan error harus generik")
			}
			if c.cookies[auth.SessionCookie] != "" {
				t.Fatal("login gagal tidak boleh membuat session")
			}
		})
	}
	if n := e.count("SELECT count(*) FROM sessions"); n != 0 {
		t.Fatalf("ada %d session padahal semua login gagal", n)
	}
	if strings.Contains(e.log.String(), "password123") || strings.Contains(e.log.String(), "salahsalah") {
		t.Fatal("log memuat password")
	}
}

func TestAPILoginValidation(t *testing.T) {
	e := setup(t)
	c := e.api()
	c.fetchCSRF()
	for _, body := range []string{`{"username":"alice"}`, `bukan json`, `{"username":"a","password":"b","extra":1}`, `{"username":"a","password":"b"}{}`} {
		expectAPIError(t, c.send("POST", "/api/login", body, c.csrf), http.StatusUnprocessableEntity, "validation_error")
	}
	big := `{"username":"` + strings.Repeat("a", 2<<20) + `","password":"x"}`
	expectAPIError(t, c.send("POST", "/api/login", big, c.csrf), http.StatusUnprocessableEntity, "validation_error")
}

func TestAPILogout(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.api()
	expectStatus(t, c.login("alice", "password123"), http.StatusOK)
	token := c.cookies[auth.SessionCookie]
	w := c.send("POST", "/api/logout", "", c.csrf)
	expectStatus(t, w, http.StatusNoContent)
	if c.cookies[auth.SessionCookie] != "" {
		t.Fatal("cookie session harus dihapus")
	}
	if e.count("SELECT count(*) FROM sessions WHERE token_hash = $1 AND revoked_at IS NOT NULL", auth.HashToken(token)) != 1 {
		t.Fatal("session harus dicabut di database")
	}
	expectAPIError(t, c.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "unauthenticated")
	c.cookies[auth.SessionCookie] = token
	expectAPIError(t, c.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
}

func TestAPILogoutRequiresLogin(t *testing.T) {
	e := setup(t)
	c := e.api()
	c.fetchCSRF()
	expectAPIError(t, c.send("POST", "/api/logout", "", c.csrf), http.StatusUnauthorized, "unauthenticated")
}

func TestAPICSRFRejected(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.api()
	c.fetchCSRF()
	body := `{"username":"alice","password":"password123"}`
	expectAPIError(t, c.send("POST", "/api/login", body, ""), http.StatusForbidden, "csrf_invalid")
	expectAPIError(t, c.send("POST", "/api/login", body, "palsu"), http.StatusForbidden, "csrf_invalid")
	other := e.api()
	other.fetchCSRF()
	expectAPIError(t, c.send("POST", "/api/login", body, other.csrf), http.StatusForbidden, "csrf_invalid")
	if c.cookies[auth.SessionCookie] != "" || e.count("SELECT count(*) FROM sessions") != 0 {
		t.Fatal("login tanpa CSRF tidak boleh berhasil")
	}
	preCSRF := c.csrf
	expectStatus(t, c.login("alice", "password123"), http.StatusOK)
	expectAPIError(t, c.send("POST", "/api/logout", "", ""), http.StatusForbidden, "csrf_invalid")
	expectAPIError(t, c.send("POST", "/api/logout", "", preCSRF), http.StatusForbidden, "csrf_invalid")
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
	if e.count("SELECT count(*) FROM sessions WHERE revoked_at IS NULL") != 1 {
		t.Fatal("logout tanpa CSRF valid tidak boleh mencabut session")
	}
}

func TestAPIMeUnauthorized(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	expectAPIError(t, e.api().send("GET", "/api/me", "", ""), http.StatusUnauthorized, "unauthenticated")
	c := e.api()
	expectStatus(t, c.login("alice", "password123"), http.StatusOK)
	e.advance(31 * time.Minute)
	expectAPIError(t, c.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
}

func TestAPIUnknownRouteIsJSON404(t *testing.T) {
	e := setup(t)
	expectAPIError(t, e.api().send("GET", "/api/tidak-ada", "", ""), http.StatusNotFound, "not_found")
}

func TestSPAServedUnderApp(t *testing.T) {
	e := setup(t)
	w := e.api().send("GET", "/app/login", "", "")
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
	r := e.api().send("GET", "/app", "", "")
	if r.Code != http.StatusMovedPermanently || r.Header().Get("Location") != "/app/" {
		t.Fatalf("redirect /app: %d %q", r.Code, r.Header().Get("Location"))
	}
	expectStatus(t, e.api().send("GET", "/login", "", ""), http.StatusOK)
	expectStatus(t, e.api().send("POST", "/app/login", "", ""), http.StatusForbidden)
}

func TestSPAPageLoadKeepsExpiredSignal(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.api()
	expectStatus(t, c.login("alice", "password123"), http.StatusOK)
	e.advance(31 * time.Minute)
	w := c.send("GET", "/app/", "", "")
	if sessionSetCookie(w) != "" {
		t.Fatal("memuat halaman SPA tidak boleh menyentuh cookie session")
	}
	expectAPIError(t, c.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
}
