package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestAPIIdleTimeout(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.apiLogin("alice", "password123")
	e.advance(20 * time.Minute)
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
	e.advance(20 * time.Minute)
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
	e.advance(31 * time.Minute)
	expectAPIError(t, c.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
	if c.session() != "" {
		t.Fatal("cookie session harus dihapus")
	}
}

func TestAPIAbsoluteExpirationNotExtendedByRotation(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.apiLogin("alice", "password123")
	var before time.Time
	e.db.QueryRow(context.Background(), "SELECT expires_at FROM sessions").Scan(&before)
	rotations := 0
	for elapsed := 10 * time.Minute; elapsed < 8*time.Hour; elapsed += 10 * time.Minute {
		e.advance(10 * time.Minute)
		old := c.session()
		expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
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
	expectAPIError(t, c.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
}

func TestAPIPeriodicRotationWithGracePeriod(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.apiLogin("alice", "password123")
	oldToken := c.session()
	e.advance(16 * time.Minute)
	w := c.send("GET", "/api/me", "", "")
	expectStatus(t, w, http.StatusOK)
	newToken := c.session()
	if newToken == "" || newToken == oldToken {
		t.Fatal("session id tidak dirotasi")
	}
	stale := c.withSession(oldToken)
	e.advance(10 * time.Second)
	w = stale.send("GET", "/api/me", "", "")
	expectStatus(t, w, http.StatusOK)
	if sessionSetCookie(w) != "" {
		t.Fatal("identifier lama dalam grace period tidak boleh memicu rotasi baru")
	}
	expectStatus(t, stale.json("POST", "/api/logout", nil), http.StatusNoContent)
	expectAPIError(t, c.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
}

func TestAPIGracePeriodEnds(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.apiLogin("alice", "password123")
	oldToken := c.session()
	e.advance(16 * time.Minute)
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
	stale := c.withSession(oldToken)
	e.advance(35 * time.Second)
	expectAPIError(t, stale.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
}

func TestAPILoginRotatesSession(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	c := e.apiLogin("alice", "password123")
	first := c.session()
	old := c.withSession(first)
	expectStatus(t, c.json("POST", "/api/login", map[string]string{"username": "alice", "password": "password123"}), http.StatusOK)
	if c.session() == "" || c.session() == first {
		t.Fatal("login ulang harus menghasilkan session id baru")
	}
	expectAPIError(t, old.send("GET", "/api/me", "", ""), http.StatusUnauthorized, "session_expired")
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
}

func TestAPIStateChangeWithoutCSRFRejected(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	c := e.apiLogin("admin", "password123")
	expectAPIError(t, c.send("POST", "/api/admin/groups", `{"name":"tanpa-token"}`, ""), http.StatusForbidden, "csrf_invalid")
	expectAPIError(t, c.send("POST", "/api/admin/groups", `{"name":"token-salah"}`, "x"), http.StatusForbidden, "csrf_invalid")
	expectAPIError(t, c.send("DELETE", "/api/admin/groups/1", "", ""), http.StatusForbidden, "csrf_invalid")
	if e.count("SELECT count(*) FROM groups") != 0 {
		t.Fatal("grup dibuat tanpa CSRF valid")
	}
	expectStatus(t, c.json("POST", "/api/admin/groups", map[string]string{"name": "dengan-token"}), http.StatusCreated)
	if e.count("SELECT count(*) FROM groups") != 1 {
		t.Fatal("grup dengan CSRF valid gagal dibuat")
	}
}
