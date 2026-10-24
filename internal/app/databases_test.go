package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func (e *testEnv) member(username string, connID int64, level authz.Level) *client {
	e.t.Helper()
	gid := testutil.SeedGroup(e.t, e.db, "grup-"+username)
	c := e.loginAs(username, false)
	testutil.SeedMember(e.t, e.db, gid, e.userID(username))
	testutil.SeedGrant(e.t, e.db, gid, connID, level)
	return c
}

const viewEdit = "View &#43; Edit"

func contains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Fatalf("response tidak memuat %q", w)
		}
	}
}

func notContains(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(body, u) {
			t.Fatalf("response seharusnya tidak memuat %q", u)
		}
	}
}

func TestDatabaseListShowsOnlyAccessibleWithEffectiveLevel(t *testing.T) {
	e := setup(t)
	a := testutil.SeedConnection(t, e.db)
	b := testutil.SeedConnection(t, e.db)
	c := testutil.SeedConnection(t, e.db)
	alice := e.loginAs("alice", false)
	viewers := testutil.SeedGroup(t, e.db, "viewers")
	editors := testutil.SeedGroup(t, e.db, "editors")
	testutil.SeedMember(t, e.db, viewers, e.userID("alice"))
	testutil.SeedMember(t, e.db, editors, e.userID("alice"))
	testutil.SeedGrant(t, e.db, viewers, a.ID, authz.LevelView)
	testutil.SeedGrant(t, e.db, viewers, b.ID, authz.LevelView)
	testutil.SeedGrant(t, e.db, editors, b.ID, authz.LevelEdit)

	w := alice.do("GET", "/databases", nil)
	expectStatus(t, w, http.StatusOK)
	body := w.Body.String()
	contains(t, body, a.Name, b.Name, "View Only", viewEdit, fmt.Sprintf(`href="/databases/%d"`, a.ID))
	notContains(t, body, c.Name, "Anda belum memiliki akses database")
	if strings.Count(body, viewEdit) != 1 {
		t.Fatal("database b harus tampil sekali dengan level tertinggi")
	}
	contains(t, body, `href="/databases"`)
}

func TestDatabaseListEmptyStates(t *testing.T) {
	e := setup(t)
	testutil.SeedConnection(t, e.db)
	alice := e.loginAs("alice", false)
	admin := e.loginAs("admin", true)
	for name, c := range map[string]*client{"user tanpa grup": alice, "admin tanpa grant": admin} {
		w := c.do("GET", "/databases", nil)
		expectStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), "Anda belum memiliki akses database") {
			t.Fatalf("%s: pesan kosong tidak tampil", name)
		}
	}
	expectStatus(t, e.client().do("GET", "/databases", nil), http.StatusSeeOther)
}

func TestDatabaseViewerSchemaAndData(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	alice := e.member("alice", conn.ID, authz.LevelView)
	base := fmt.Sprintf("/databases/%d", conn.ID)

	w := alice.do("GET", base, nil)
	expectStatus(t, w, http.StatusOK)
	contains(t, w.Body.String(), "customers", "order_items", "order_totals", "legacy_notes", "Legacy Reports", "Schema analytics", "events", "View Only",
		base+"/migrations", base+"/tables/public/Legacy%20Reports", base+"/tables/analytics/events")

	w = alice.do("GET", base+"/tables/public/customers", nil)
	expectStatus(t, w, http.StatusOK)
	body := w.Body.String()
	contains(t, body, "Customer 001", "customer1@example.test", "PRIMARY KEY", "customers_email_key", "customers_city_idx", "customers_pkey",
		"Menampilkan 1-25 dari 200 baris", "berdasarkan primary key", "Berikutnya", "page=2&amp;page_size=25", "timestamp with time zone")
	notContains(t, body, "Customer 026", "Sebelumnya")

	for _, tc := range []struct{ query, want string }{
		{"?page=8", "176-200 dari 200"},
		{"?page=999", "176-200 dari 200"},
		{"?page=0", "Menampilkan 1-25"},
		{"?page=-3", "Menampilkan 1-25"},
		{"?page=abc", "Menampilkan 1-25"},
		{"?page_size=1000", "Menampilkan 1-100"},
		{"?page_size=0", "Menampilkan 1-25"},
		{"?page_size=-5", "Menampilkan 1-25"},
		{"?page_size=zzz", "Menampilkan 1-25"},
		{"?page=2&page_size=10", "11-20 dari 200"},
	} {
		w := alice.do("GET", base+"/tables/public/customers"+tc.query, nil)
		expectStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: tidak memuat %q", tc.query, tc.want)
		}
	}
	last := alice.do("GET", base+"/tables/public/customers?page=8", nil).Body.String()
	contains(t, last, "Customer 200", "Sebelumnya")
	notContains(t, last, "Berikutnya", "Customer 175")
	big := alice.do("GET", base+"/tables/public/customers?page_size=1000", nil).Body.String()
	contains(t, big, "Customer 100")
	notContains(t, big, "Customer 101")

	w = alice.do("GET", base+"/tables/public/legacy_notes", nil)
	expectStatus(t, w, http.StatusOK)
	body = w.Body.String()
	contains(t, body, "ctid", "&lt;b&gt;tag HTML&lt;/b&gt;", "&amp; karakter", `class="null">NULL<`, `title="Teks dipotong"`, "andi")
	notContains(t, body, "<b>tag HTML</b>", strings.Repeat("teks sangat panjang ", 30))

	w = alice.do("GET", base+"/tables/public/order_totals", nil)
	expectStatus(t, w, http.StatusOK)
	contains(t, w.Body.String(), "tidak punya urutan yang stabil", "total", "View")

	w = alice.do("GET", base+"/tables/public/Legacy%20Reports", nil)
	expectStatus(t, w, http.StatusOK)
	contains(t, w.Body.String(), "Report Title", "Laporan Q1", "Laporan &#34;Tahunan&#34;")
	w = alice.do("GET", base+"/tables/public/order_items", nil)
	expectStatus(t, w, http.StatusOK)
	contains(t, w.Body.String(), "PRIMARY KEY (order_id, product_id)")
	w = alice.do("GET", base+"/tables/analytics/events", nil)
	expectStatus(t, w, http.StatusOK)
	contains(t, w.Body.String(), "dari 250 baris", "jsonb", "events_kind_idx")

	for _, p := range []string{"/tables/public/tidak_ada", "/tables/pg_catalog/pg_class", "/tables/public/pg_class", "/tables/public/customers%3Bdrop", "/tables/nope/customers", "/tables/public/customers%22"} {
		expectStatus(t, alice.do("GET", base+p, nil), http.StatusNotFound)
	}
	expectStatus(t, alice.do("GET", "/databases/abc", nil), http.StatusNotFound)
	expectStatus(t, alice.do("GET", "/databases/0/tables/public/customers", nil), http.StatusNotFound)
}

func TestDatabaseViewerAccessControl(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	other := testutil.SeedConnection(t, e.db)
	viewer := e.member("viewer", conn.ID, authz.LevelView)
	editor := e.member("editor", conn.ID, authz.LevelEdit)
	noGrant := e.loginAs("tanpagrant", false)
	outsider := e.member("luar", other.ID, authz.LevelEdit)
	admin := e.loginAs("admin", true)
	emptyGroup := testutil.SeedGroup(t, e.db, "kosong")
	member := e.loginAs("anggota", false)
	testutil.SeedMember(t, e.db, emptyGroup, e.userID("anggota"))
	testutil.SeedGrant(t, e.db, emptyGroup, other.ID, authz.LevelView)

	pages := []string{fmt.Sprintf("/databases/%d", conn.ID), fmt.Sprintf("/databases/%d/tables/public/customers", conn.ID)}
	for _, p := range pages {
		expectStatus(t, viewer.do("GET", p, nil), http.StatusOK)
		expectStatus(t, editor.do("GET", p, nil), http.StatusOK)
		for name, c := range map[string]*client{"tanpa grant": noGrant, "grant ke database lain": outsider, "anggota grup lain": member, "admin tanpa grant": admin} {
			w := c.do("GET", p, nil)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s mengakses %s: status %d, want 403", name, p, w.Code)
			}
			notContains(t, w.Body.String(), "Customer 001", "customers_pkey")
		}
		w := e.client().do("GET", p, nil)
		expectStatus(t, w, http.StatusSeeOther)
	}
	for _, c := range []*client{noGrant, admin} {
		expectStatus(t, c.do("GET", "/databases/99999", nil), http.StatusForbidden)
		expectStatus(t, c.do("GET", "/databases/99999/tables/public/customers", nil), http.StatusForbidden)
	}
	expectStatus(t, viewer.do("GET", "/databases/99999", nil), http.StatusForbidden)

	gid := testutil.SeedGroup(t, e.db, "sementara")
	temp := e.loginAs("sementara", false)
	testutil.SeedMember(t, e.db, gid, e.userID("sementara"))
	testutil.SeedGrant(t, e.db, gid, conn.ID, authz.LevelView)
	expectStatus(t, temp.do("GET", pages[0], nil), http.StatusOK)
	if err := e.app.Grants.RemoveAccess(context.Background(), gid, conn.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, temp.do("GET", pages[0], nil), http.StatusUnauthorized)

	if err := e.app.Users.Update(context.Background(), e.userID("viewer"), "viewer", false, false); err != nil {
		t.Fatal(err)
	}
	if w := viewer.do("GET", pages[0], nil); w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("user nonaktif status %d", w.Code)
	}
}

func TestDatabaseConnectionErrorPage(t *testing.T) {
	e := setup(t)
	dead := testutil.SeedConnection(t, e.db, func(in *dbconn.Input) { in.Port = closedPort; in.Password = secretPW })
	live := testutil.SeedConnection(t, e.db)
	alice := e.member("alice", dead.ID, authz.LevelEdit)
	liveGroup := testutil.SeedGroup(t, e.db, "hidup")
	testutil.SeedMember(t, e.db, liveGroup, e.userID("alice"))
	testutil.SeedGrant(t, e.db, liveGroup, live.ID, authz.LevelView)
	admin := e.loginAs("admin", true)
	adminGroup := testutil.SeedGroup(t, e.db, "grup-admin")
	testutil.SeedMember(t, e.db, adminGroup, e.userID("admin"))
	testutil.SeedGrant(t, e.db, adminGroup, dead.ID, authz.LevelView)

	base := fmt.Sprintf("/databases/%d", dead.ID)
	for _, p := range []string{base, base + "/tables/public/customers"} {
		w := alice.do("GET", p, nil)
		expectStatus(t, w, http.StatusServiceUnavailable)
		body := w.Body.String()
		contains(t, body, dead.Name, dbconn.KindRefused.UserMessage(), "Aplikasi tetap berjalan", "Coba lagi")
		notContains(t, body, "127.0.0.1", fmt.Sprint(closedPort), secretPW, "dial tcp", "connect:", "/admin/connections/")
		w = admin.do("GET", p, nil)
		expectStatus(t, w, http.StatusServiceUnavailable)
		contains(t, w.Body.String(), fmt.Sprintf("/admin/connections/%d", dead.ID))
		notContains(t, w.Body.String(), "127.0.0.1", secretPW)
	}
	expectStatus(t, alice.do("GET", "/", nil), http.StatusOK)
	expectStatus(t, alice.do("GET", "/databases", nil), http.StatusOK)
	expectStatus(t, alice.do("GET", fmt.Sprintf("/databases/%d", live.ID), nil), http.StatusOK)
	logs := e.log.String()
	contains(t, logs, "target database unreachable")
	notContains(t, logs, secretPW, "127.0.0.1:5499")
}

func TestConnectionUpdateInvalidatesPool(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	alice := e.member("alice", conn.ID, authz.LevelView)
	admin := e.loginAs("admin", true)
	page := fmt.Sprintf("/databases/%d", conn.ID)
	update := func(port int) {
		t.Helper()
		v := connValues(conn.Name)
		v.Set("password", "")
		v.Set("port", fmt.Sprint(port))
		expectStatus(t, admin.post(fmt.Sprintf("/admin/connections/%d", conn.ID), v), http.StatusSeeOther)
	}
	expectStatus(t, alice.do("GET", page, nil), http.StatusOK) // pool dibuat dan di-cache
	update(closedPort)
	expectStatus(t, alice.do("GET", page, nil), http.StatusServiceUnavailable)
	update(5435)
	expectStatus(t, alice.do("GET", page, nil), http.StatusOK)
	e.noSecretInLogs()
}
