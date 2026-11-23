package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func expectStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	testutil.ExpectStatus(t, w, want)
}

func sessionSetCookie(w *httptest.ResponseRecorder) string {
	return testutil.SessionSetCookie(w)
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

func TestSecureCookieInProduction(t *testing.T) {
	e := setupWith(t, true)
	e.createUser("alice", "password123", false)
	w := e.api().login("alice", "password123")
	expectStatus(t, w, http.StatusOK)
	if !strings.Contains(sessionSetCookie(w), "Secure") {
		t.Fatal("production harus memakai cookie Secure")
	}
}

func TestSecretsNeverLogged(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.api()
	c.login("alice", "rahasia-salah-xyz")
	expectStatus(t, c.login("alice", "password123"), http.StatusOK)
	token := c.session()
	c.send("GET", "/api/me?q=1", "", "")
	expectStatus(t, c.json("POST", "/api/logout", nil), http.StatusNoContent)
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
	expectStatus(t, e.api().login("admin", "password123"), http.StatusOK)
}
