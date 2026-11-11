package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func TestOldAdminPagesRedirectToSPA(t *testing.T) {
	e := setup(t)
	admin := e.loginAs("admin", true)
	bob := e.createUser("bob", "password123", false)
	cases := map[string]string{
		"/admin/users": "/app/admin/user",
		"/admin/users/new": "/app/admin/user/baru",
		fmt.Sprintf("/admin/users/%d", bob.ID): fmt.Sprintf("/app/admin/user/%d", bob.ID),
		"/admin/groups": "/app/admin/grup",
	}
	for from, to := range cases {
		w := admin.do("GET", from, nil)
		expectStatus(t, w, http.StatusSeeOther)
		if loc := w.Header().Get("Location"); loc != to {
			t.Fatalf("%s diarahkan ke %q, want %q", from, loc, to)
		}
	}
	alice := e.loginAs("alice", false)
	anon := e.client()
	for from := range cases {
		expectStatus(t, alice.do("GET", from, nil), http.StatusForbidden)
		w := anon.do("GET", from, nil)
		expectStatus(t, w, http.StatusSeeOther)
		if w.Header().Get("Location") != "/login" {
			t.Fatalf("%s: anonim tidak diarahkan ke /login", from)
		}
	}
}

func TestOldAdminFormsRemoved(t *testing.T) {
	e := setup(t)
	admin := e.loginAs("admin", true)
	bob := e.createUser("bob", "password123", false)
	gid := testutil.SeedGroup(t, e.db, "tim")
	testutil.SeedMember(t, e.db, gid, bob.ID)
	posts := map[string]url.Values{
		"/admin/users": {"username": {"carol"}, "password": {"password123"}},
		fmt.Sprintf("/admin/users/%d", bob.ID): {"username": {"bob"}, "is_admin": {"on"}, "is_active": {"on"}},
		fmt.Sprintf("/admin/users/%d/password", bob.ID): {"password": {"passwordbaru"}},
		"/admin/groups": {"name": {"baru"}},
		fmt.Sprintf("/admin/groups/%d", gid): {"name": {"ganti"}},
		fmt.Sprintf("/admin/groups/%d/delete", gid): {},
		fmt.Sprintf("/admin/groups/%d/members", gid): {"user_id": {fmt.Sprint(bob.ID)}},
		fmt.Sprintf("/admin/groups/%d/members/%d/delete", gid, bob.ID): {},
	}
	for path, form := range posts {
		expectStatus(t, admin.post(path, form), http.StatusNotFound)
	}
	if e.count("SELECT count(*) FROM users") != 2 || e.count("SELECT count(*) FROM users WHERE is_admin") != 1 || e.count("SELECT count(*) FROM groups WHERE name = 'tim'") != 1 || e.count("SELECT count(*) FROM group_members") != 1 {
		t.Fatal("form lama masih mengubah data")
	}
}

func TestOldGroupPageLinksToSPAAndAccess(t *testing.T) {
	e := setup(t)
	admin := e.loginAs("admin", true)
	gid := testutil.SeedGroup(t, e.db, "tim <data>")
	w := admin.do("GET", fmt.Sprintf("/admin/groups/%d", gid), nil)
	expectStatus(t, w, http.StatusOK)
	body := w.Body.String()
	for _, want := range []string{fmt.Sprintf(`href="/app/admin/grup/%d"`, gid), fmt.Sprintf(`href="/admin/groups/%d/access"`, gid), "tim &lt;data&gt;", `href="/app/admin/user"`, `href="/app/admin/grup"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("halaman grup tidak memuat %q", want)
		}
	}
	if strings.Contains(body, `method="post" action="/admin/groups`) {
		t.Fatal("halaman grup masih punya form lama")
	}
	expectStatus(t, admin.do("GET", "/admin/groups/99999", nil), http.StatusNotFound)
	expectStatus(t, e.loginAs("alice", false).do("GET", fmt.Sprintf("/admin/groups/%d", gid), nil), http.StatusForbidden)
}
