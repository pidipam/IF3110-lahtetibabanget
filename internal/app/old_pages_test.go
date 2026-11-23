package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func TestOldPagesRedirectToSPA(t *testing.T) {
	e := setup(t)
	cases := map[string]string{
		"/": "/app/",
		"/login": "/app/login",
		"/admin/users": "/app/admin/user",
		"/admin/users/new": "/app/admin/user/baru",
		"/admin/users/7": "/app/admin/user/7",
		"/admin/groups": "/app/admin/grup",
		"/admin/groups/7": "/app/admin/grup/7",
		"/admin/groups/7/access": "/app/admin/grup/7",
		"/admin/connections": "/app/admin/koneksi",
		"/admin/connections/new": "/app/admin/koneksi/baru",
		"/admin/connections/7": "/app/admin/koneksi/7",
		"/databases": "/app/database",
		"/databases/3": "/app/database/3",
		"/databases/3/schema-diff": "/app/migration/database/3/schema-diff",
		"/databases/3/migrations": "/app/migration/database/3",
		"/databases/3/migrations/new": "/app/migration/database/3/baru",
		"/databases/3/migrations/9": "/app/migration/9",
		"/databases/3/migrations/9/edit": "/app/migration/9/ubah",
		"/databases/3/migrations/9/review": "/app/migration/9",
	}
	anon := e.api()
	admin := e.apiUser("admin", true)
	for _, c := range []*apiClient{anon, admin} {
		for from, to := range cases {
			w := c.send("GET", from, "", "")
			expectStatus(t, w, http.StatusSeeOther)
			if loc := w.Header().Get("Location"); loc != to {
				t.Fatalf("%s diarahkan ke %q, want %q", from, loc, to)
			}
		}
	}
	for _, path := range []string{"/admin/users/abc", "/databases/x/migrations", "/halaman-lama"} {
		w := anon.send("GET", path, "", "")
		expectStatus(t, w, http.StatusSeeOther)
		if loc := w.Header().Get("Location"); loc != "/app/error/404" {
			t.Fatalf("%s diarahkan ke %q", path, loc)
		}
	}
}

func TestOldFormsRemoved(t *testing.T) {
	e := setup(t)
	admin := e.apiUser("admin", true)
	bob := e.createUser("bob", "password123", false)
	gid := testutil.SeedGroup(t, e.db, "tim")
	testutil.SeedMember(t, e.db, gid, bob.ID)
	conn := testutil.SeedConnection(t, e.db)
	testutil.SeedGrant(t, e.db, gid, conn.ID, 2)
	paths := []string{
		"/login",
		"/logout",
		"/admin/users",
		fmt.Sprintf("/admin/users/%d", bob.ID),
		fmt.Sprintf("/admin/users/%d/password", bob.ID),
		"/admin/groups",
		fmt.Sprintf("/admin/groups/%d", gid),
		fmt.Sprintf("/admin/groups/%d/delete", gid),
		fmt.Sprintf("/admin/groups/%d/members", gid),
		fmt.Sprintf("/databases/%d/migrations", conn.ID),
		fmt.Sprintf("/databases/%d/migrations/1/execute", conn.ID),
		fmt.Sprintf("/databases/%d/migrations/1/explain", conn.ID),
		fmt.Sprintf("/databases/%d/schema-diff", conn.ID),
		fmt.Sprintf("/databases/%d/schema-diff/migration", conn.ID),
	}
	for _, path := range paths {
		w := admin.send("POST", path, "title=x&sql=SELECT+1", admin.csrf)
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s status %d, want 404 atau 405", path, w.Code)
		}
	}
	if e.count("SELECT count(*) FROM users") != 2 || e.count("SELECT count(*) FROM groups WHERE name = 'tim'") != 1 || e.count("SELECT count(*) FROM group_members") != 1 || e.count("SELECT count(*) FROM migrations") != 0 || e.count("SELECT count(*) FROM platform_migrations") != 0 {
		t.Fatal("form lama masih mengubah data")
	}
}

func TestNoServerRenderedPages(t *testing.T) {
	e := setup(t)
	admin := e.apiUser("admin", true)
	for _, path := range []string{"/", "/login", "/admin/groups", "/databases"} {
		w := admin.send("GET", path, "", "")
		if strings.Contains(w.Body.String(), "<form") || strings.Contains(w.Body.String(), "csrf_token") {
			t.Fatalf("%s masih merender halaman HTML lama", path)
		}
	}
}
