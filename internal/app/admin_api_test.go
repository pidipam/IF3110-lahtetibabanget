package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/adminapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/sessionapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func userPath(id int64) string {
	return fmt.Sprintf("/api/admin/users/%d", id)
}

func groupPath(id int64) string {
	return fmt.Sprintf("/api/admin/groups/%d", id)
}

func levelsOf(t *testing.T, c *apiClient) map[int64]string {
	t.Helper()
	w := c.send("GET", "/api/me", "", "")
	expectStatus(t, w, http.StatusOK)
	var me sessionapi.MeResponse
	decodeBody(t, w, &me)
	out := map[int64]string{}
	for _, d := range me.Databases {
		out[d.ID] = d.Level
	}
	return out
}

func expectRotated(t *testing.T, c *apiClient, what string) {
	t.Helper()
	before := c.session()
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
	if c.session() == "" || c.session() == before {
		t.Fatalf("session tidak dirotasi setelah %s", what)
	}
}

func expectRevoked(t *testing.T, c *apiClient, what string) {
	t.Helper()
	w := c.send("GET", "/api/me", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("session masih hidup setelah %s: status %d", what, w.Code)
	}
}

func TestAPIAdminNonAdminForbidden(t *testing.T) {
	e := setup(t)
	e.createUser("alice", "password123", false)
	target := e.createUser("budi", "password123", false)
	g := testutil.SeedGroup(t, e.db, "tim")
	conn := testutil.SeedConnection(t, e.db)
	type route struct {
		method, path string
		body any
	}
	routes := []route{
		{"GET", "/api/admin/users", nil},
		{"POST", "/api/admin/users", map[string]any{"username": "ilegal", "password": "password123", "is_admin": true}},
		{"GET", userPath(target.ID), nil},
		{"PATCH", userPath(target.ID), map[string]any{"is_admin": true}},
		{"POST", userPath(target.ID) + "/password", map[string]any{"password": "passwordbaru"}},
		{"GET", "/api/admin/groups", nil},
		{"POST", "/api/admin/groups", map[string]any{"name": "ilegal"}},
		{"GET", groupPath(g), nil},
		{"PATCH", groupPath(g), map[string]any{"name": "ilegal"}},
		{"DELETE", groupPath(g), nil},
		{"GET", groupPath(g) + "/candidates", nil},
		{"POST", groupPath(g) + "/members", map[string]any{"user_id": target.ID}},
		{"DELETE", fmt.Sprintf("%s/members/%d", groupPath(g), target.ID), nil},
		{"GET", groupPath(g) + "/access", nil},
		{"PUT", groupPath(g) + "/access", map[string]any{"access": []map[string]any{{"db_id": conn.ID, "level": "edit"}}}},
	}
	alice := e.apiLogin("alice", "password123")
	anon := e.api()
	anon.fetchCSRF()
	for _, rt := range routes {
		expectAPIError(t, alice.json(rt.method, rt.path, rt.body), http.StatusForbidden, "forbidden")
		expectAPIError(t, anon.json(rt.method, rt.path, rt.body), http.StatusUnauthorized, "unauthenticated")
	}
	if e.count("SELECT count(*) FROM users WHERE is_admin") != 0 || e.count("SELECT count(*) FROM users") != 2 {
		t.Fatal("non-admin berhasil mengubah user")
	}
	if e.count("SELECT count(*) FROM groups WHERE name = 'tim'") != 1 || e.count("SELECT count(*) FROM group_members") != 0 || e.count("SELECT count(*) FROM group_db_access") != 0 {
		t.Fatal("non-admin berhasil mengubah grup")
	}
	expectStatus(t, e.api().login("budi", "password123"), http.StatusOK)
}

func TestAPIAdminManagesUsers(t *testing.T) {
	e := setup(t)
	root := e.createUser("admin", "password123", true)
	c := e.apiLogin("admin", "password123")
	w := c.json("POST", "/api/admin/users", map[string]any{"username": " Carol ", "password": "password123"})
	expectStatus(t, w, http.StatusCreated)
	var carol adminapi.UserDTO
	decodeBody(t, w, &carol)
	if carol.ID == 0 || carol.Username != "carol" || carol.IsAdmin || !carol.IsActive || carol.CreatedAt.Location() != time.UTC {
		t.Fatalf("user baru salah: %+v", carol)
	}
	if strings.Contains(w.Body.String(), "password") {
		t.Fatal("response memuat password atau hash")
	}
	expectAPIError(t, c.json("POST", "/api/admin/users", map[string]any{"username": "carol", "password": "password123"}), http.StatusConflict, "conflict")
	expectInvalidField(t, c.json("POST", "/api/admin/users", map[string]any{"username": "x", "password": "password123"}), "username")
	expectInvalidField(t, c.json("POST", "/api/admin/users", map[string]any{"username": "dodi", "password": "pendek"}), "password")
	expectAPIError(t, c.json("POST", "/api/admin/users", map[string]any{"username": "dodi", "password": "password123", "role": "admin"}), http.StatusUnprocessableEntity, "validation_error")
	for _, name := range []string{"dina", "dodo", "eko"} {
		expectStatus(t, c.json("POST", "/api/admin/users", map[string]any{"username": name, "password": "password123"}), http.StatusCreated)
	}

	var page httpapi.Page[adminapi.UserDTO]
	w = c.send("GET", "/api/admin/users?page=2&page_size=2", "", "")
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &page)
	if page.Total != 5 || page.Page != 2 || page.PageSize != 2 || len(page.Items) != 2 || page.Items[0].Username != "dina" || page.Items[1].Username != "dodo" {
		t.Fatalf("paginasi salah: %+v", page)
	}
	w = c.send("GET", "/api/admin/users?q=D", "", "")
	decodeBody(t, w, &page)
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("pencarian salah: %+v", page)
	}
	w = c.send("GET", "/api/admin/users?page=9", "", "")
	decodeBody(t, w, &page)
	if page.Total != 5 || len(page.Items) != 0 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("halaman kosong salah: %s", w.Body.String())
	}
	expectInvalidField(t, c.send("GET", "/api/admin/users?page_size=500", "", ""), "page_size")

	g := testutil.SeedGroup(t, e.db, "tim-data")
	testutil.SeedMember(t, e.db, g, carol.ID)
	w = c.send("GET", userPath(carol.ID), "", "")
	expectStatus(t, w, http.StatusOK)
	var detail adminapi.UserDetail
	decodeBody(t, w, &detail)
	if detail.Username != "carol" || len(detail.Groups) != 1 || detail.Groups[0].Name != "tim-data" {
		t.Fatalf("detail user salah: %+v", detail)
	}

	w = c.json("PATCH", userPath(carol.ID), map[string]any{"username": "Caroline"})
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &detail)
	if detail.Username != "caroline" || detail.IsAdmin || !detail.IsActive || len(detail.Groups) != 1 {
		t.Fatalf("hasil ubah salah: %+v", detail)
	}
	expectAPIError(t, c.json("PATCH", userPath(carol.ID), map[string]any{"username": "dina"}), http.StatusConflict, "conflict")
	expectInvalidField(t, c.json("PATCH", userPath(carol.ID), map[string]any{"username": "!!"}), "username")
	expectAPIError(t, c.json("PATCH", userPath(carol.ID), map[string]any{"is_admin": "ya"}), http.StatusUnprocessableEntity, "validation_error")

	expectAPIError(t, c.send("GET", "/api/admin/users/9999", "", ""), http.StatusNotFound, "not_found")
	expectAPIError(t, c.send("GET", "/api/admin/users/abc", "", ""), http.StatusNotFound, "not_found")
	expectAPIError(t, c.json("PATCH", "/api/admin/users/9999", map[string]any{"is_admin": true}), http.StatusNotFound, "not_found")
	expectAPIError(t, c.json("POST", "/api/admin/users/9999/password", map[string]any{"password": "password123"}), http.StatusNotFound, "not_found")
	expectInvalidField(t, c.json("POST", userPath(carol.ID)+"/password", map[string]any{"password": "pendek"}), "password")
	if e.count("SELECT count(*) FROM users WHERE id = $1 AND is_admin", root.ID) != 1 {
		t.Fatal("admin berubah tanpa diminta")
	}
}

func TestAPIAdminManagesGroups(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	carol := e.createUser("carol", "password123", false)
	off := e.createUser("off", "password123", false)
	e.app.Users.Update(context.Background(), off.ID, "off", false, false)
	c := e.apiLogin("admin", "password123")

	w := c.json("POST", "/api/admin/groups", map[string]any{"name": " Tim Data ", "description": "Analis"})
	expectStatus(t, w, http.StatusCreated)
	var g adminapi.GroupDTO
	decodeBody(t, w, &g)
	if g.ID == 0 || g.Name != "Tim Data" || g.Description != "Analis" || g.MemberCount != 0 {
		t.Fatalf("grup baru salah: %+v", g)
	}
	expectAPIError(t, c.json("POST", "/api/admin/groups", map[string]any{"name": "Tim Data"}), http.StatusConflict, "conflict")
	expectInvalidField(t, c.json("POST", "/api/admin/groups", map[string]any{"name": "  "}), "name")
	expectInvalidField(t, c.json("POST", "/api/admin/groups", map[string]any{"name": strings.Repeat("x", 65)}), "name")
	expectInvalidField(t, c.json("POST", "/api/admin/groups", map[string]any{"name": "ok", "description": strings.Repeat("x", 501)}), "description")
	expectStatus(t, c.json("POST", "/api/admin/groups", map[string]any{"name": "Operasional"}), http.StatusCreated)

	w = c.json("PATCH", groupPath(g.ID), map[string]any{"description": "Tim analis data"})
	expectStatus(t, w, http.StatusOK)
	var detail adminapi.GroupDetail
	decodeBody(t, w, &detail)
	if detail.Name != "Tim Data" || detail.Description != "Tim analis data" {
		t.Fatalf("hasil ubah grup salah: %+v", detail)
	}
	expectAPIError(t, c.json("PATCH", groupPath(g.ID), map[string]any{"name": "Operasional"}), http.StatusConflict, "conflict")

	var cand struct {
		Items []adminapi.UserDTO `json:"items"`
	}
	w = c.send("GET", groupPath(g.ID)+"/candidates", "", "")
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &cand)
	if len(cand.Items) != 2 {
		t.Fatalf("kandidat harus admin dan carol saja: %+v", cand.Items)
	}

	expectStatus(t, c.json("POST", groupPath(g.ID)+"/members", map[string]any{"user_id": carol.ID}), http.StatusNoContent)
	expectStatus(t, c.json("POST", groupPath(g.ID)+"/members", map[string]any{"user_id": carol.ID}), http.StatusNoContent)
	expectInvalidField(t, c.json("POST", groupPath(g.ID)+"/members", map[string]any{"user_id": off.ID}), "user_id")
	expectInvalidField(t, c.json("POST", groupPath(g.ID)+"/members", map[string]any{"user_id": 9999}), "user_id")
	expectAPIError(t, c.json("POST", "/api/admin/groups/9999/members", map[string]any{"user_id": carol.ID}), http.StatusNotFound, "not_found")

	w = c.send("GET", groupPath(g.ID), "", "")
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &detail)
	if detail.MemberCount != 1 || len(detail.Members) != 1 || detail.Members[0].Username != "carol" || detail.Access == nil {
		t.Fatalf("detail grup salah: %+v", detail)
	}
	var page httpapi.Page[adminapi.GroupDTO]
	decodeBody(t, c.send("GET", "/api/admin/groups?q=tim", "", ""), &page)
	if page.Total != 1 || page.Items[0].MemberCount != 1 {
		t.Fatalf("daftar grup salah: %+v", page)
	}

	user := e.apiLogin("carol", "password123")
	expectStatus(t, c.send("DELETE", fmt.Sprintf("%s/members/%d", groupPath(g.ID), carol.ID), "", c.csrf), http.StatusNoContent)
	if e.count("SELECT count(*) FROM group_members") != 0 {
		t.Fatal("anggota tidak dikeluarkan")
	}
	expectRevoked(t, user, "keluar grup")
	other := e.apiLogin("carol", "password123")
	expectAPIError(t, c.send("DELETE", fmt.Sprintf("%s/members/%d", groupPath(g.ID), carol.ID), "", c.csrf), http.StatusNotFound, "not_found")
	expectStatus(t, other.send("GET", "/api/me", "", ""), http.StatusOK)

	expectStatus(t, c.send("DELETE", groupPath(g.ID), "", c.csrf), http.StatusNoContent)
	expectAPIError(t, c.send("GET", groupPath(g.ID), "", ""), http.StatusNotFound, "not_found")
	expectAPIError(t, c.send("DELETE", groupPath(g.ID), "", c.csrf), http.StatusNotFound, "not_found")
	if e.count("SELECT count(*) FROM groups") != 1 {
		t.Fatal("grup tidak terhapus")
	}
}

func TestAPIPrivilegeEscalationRotatesSession(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	bob := e.createUser("bob", "password123", false)
	conn := testutil.SeedConnection(t, e.db)
	admin := e.apiLogin("admin", "password123")
	b := e.apiLogin("bob", "password123")

	g := testutil.SeedGroup(t, e.db, "analis")
	expectStatus(t, admin.json("POST", groupPath(g)+"/members", map[string]any{"user_id": bob.ID}), http.StatusNoContent)
	expectRotated(t, b, "masuk grup")

	expectStatus(t, admin.json("PUT", groupPath(g)+"/access", map[string]any{"access": []map[string]any{{"db_id": conn.ID, "level": "view"}}}), http.StatusOK)
	expectRotated(t, b, "akses naik ke view")
	if levelsOf(t, b)[conn.ID] != "view" {
		t.Fatal("akses view tidak terlihat")
	}
	expectStatus(t, admin.json("PUT", groupPath(g)+"/access", map[string]any{"access": []map[string]any{{"db_id": conn.ID, "level": "edit"}}}), http.StatusOK)
	expectRotated(t, b, "akses naik ke edit")

	expectAPIError(t, b.send("GET", "/api/admin/users", "", ""), http.StatusForbidden, "forbidden")
	expectStatus(t, admin.json("PATCH", userPath(bob.ID), map[string]any{"is_admin": true}), http.StatusOK)
	expectRotated(t, b, "jadi admin")
	expectStatus(t, b.send("GET", "/api/admin/users", "", ""), http.StatusOK)
}

func TestAPIPrivilegeDecreaseRevokesSessions(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	admin := e.apiLogin("admin", "password123")
	conn := testutil.SeedConnection(t, e.db)
	cases := []struct {
		name string
		admin bool
		act func(userID, groupID int64) *httptest.ResponseRecorder
		status int
	}{
		{"dinonaktifkan", false, func(u, g int64) *httptest.ResponseRecorder {
			return admin.json("PATCH", userPath(u), map[string]any{"is_active": false})
		}, http.StatusOK},
		{"hak admin dicabut", true, func(u, g int64) *httptest.ResponseRecorder {
			return admin.json("PATCH", userPath(u), map[string]any{"is_admin": false})
		}, http.StatusOK},
		{"password direset", false, func(u, g int64) *httptest.ResponseRecorder {
			return admin.json("POST", userPath(u)+"/password", map[string]any{"password": "passwordbaru"})
		}, http.StatusNoContent},
		{"keluar grup", false, func(u, g int64) *httptest.ResponseRecorder {
			return admin.send("DELETE", fmt.Sprintf("%s/members/%d", groupPath(g), u), "", admin.csrf)
		}, http.StatusNoContent},
		{"akses turun", false, func(u, g int64) *httptest.ResponseRecorder {
			return admin.json("PUT", groupPath(g)+"/access", map[string]any{"access": []map[string]any{{"db_id": conn.ID, "level": "view"}}})
		}, http.StatusOK},
		{"akses dicabut", false, func(u, g int64) *httptest.ResponseRecorder {
			return admin.json("PUT", groupPath(g)+"/access", map[string]any{"access": []map[string]any{{"db_id": conn.ID, "level": "none"}}})
		}, http.StatusOK},
		{"grup dihapus", false, func(u, g int64) *httptest.ResponseRecorder {
			return admin.send("DELETE", groupPath(g), "", admin.csrf)
		}, http.StatusNoContent},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := fmt.Sprintf("user%d", i)
			u := e.createUser(name, "password123", tc.admin)
			g := testutil.SeedGroup(t, e.db, "grup-"+name)
			testutil.SeedMember(t, e.db, g, u.ID)
			testutil.SeedGrant(t, e.db, g, conn.ID, authz.LevelEdit)
			bystander := e.createUser("lain"+name, "password123", false)
			victim := e.apiLogin(name, "password123")
			other := e.apiLogin(bystander.Username, "password123")
			expectStatus(t, tc.act(u.ID, g), tc.status)
			expectRevoked(t, victim, tc.name)
			expectStatus(t, other.send("GET", "/api/me", "", ""), http.StatusOK)
			expectStatus(t, admin.send("GET", "/api/me", "", ""), http.StatusOK)
		})
	}
	expectAPIError(t, e.api().login("user0", "password123"), http.StatusUnauthorized, "invalid_credentials")
	expectAPIError(t, e.api().login("user2", "password123"), http.StatusUnauthorized, "invalid_credentials")
	expectStatus(t, e.api().login("user2", "passwordbaru"), http.StatusOK)
}

func TestAPIRenameDoesNotRevokeSession(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	bob := e.createUser("bob", "password123", false)
	admin := e.apiLogin("admin", "password123")
	b := e.apiLogin("bob", "password123")
	before := b.session()
	expectStatus(t, admin.json("PATCH", userPath(bob.ID), map[string]any{"username": "bobby"}), http.StatusOK)
	expectStatus(t, b.send("GET", "/api/me", "", ""), http.StatusOK)
	if b.session() != before {
		t.Fatal("ganti username tidak perlu merotasi session")
	}
}

func TestAPIAdminCannotLockThemselvesOut(t *testing.T) {
	e := setup(t)
	me := e.createUser("admin", "password123", true)
	c := e.apiLogin("admin", "password123")
	for _, body := range []map[string]any{{"is_admin": false}, {"is_active": false}, {"username": "root", "is_admin": false, "is_active": false}} {
		expectAPIError(t, c.json("PATCH", userPath(me.ID), body), http.StatusUnprocessableEntity, "self_lockout")
	}
	if e.count("SELECT count(*) FROM users WHERE username = 'admin' AND is_admin AND is_active") != 1 {
		t.Fatal("admin berhasil mencabut hak dirinya sendiri")
	}
	expectStatus(t, c.send("GET", "/api/me", "", ""), http.StatusOK)
	expectStatus(t, c.json("PATCH", userPath(me.ID), map[string]any{"username": "root", "is_admin": true}), http.StatusOK)
}

func TestAPIAdminResetOwnPasswordEndsSession(t *testing.T) {
	e := setup(t)
	me := e.createUser("admin", "password123", true)
	c := e.apiLogin("admin", "password123")
	w := c.json("POST", userPath(me.ID)+"/password", map[string]any{"password": "passwordbaru"})
	expectStatus(t, w, http.StatusNoContent)
	if !strings.Contains(sessionSetCookie(w), "Max-Age=0") || c.session() != "" {
		t.Fatalf("cookie session harus dihapus: %q", sessionSetCookie(w))
	}
	expectStatus(t, e.api().login("admin", "passwordbaru"), http.StatusOK)
}

func TestAPIEffectiveAccess(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.createUser("admin", "password123", true)
	alice := e.createUser("alice", "password123", false)
	admin := e.apiLogin("admin", "password123")
	viewers := testutil.SeedGroup(t, e.db, "viewers")
	editors := testutil.SeedGroup(t, e.db, "editors")
	testutil.SeedMember(t, e.db, viewers, alice.ID)
	testutil.SeedMember(t, e.db, editors, alice.ID)
	db1 := testutil.SeedConnection(t, e.db).ID
	db2 := testutil.SeedConnection(t, e.db).ID
	db3 := testutil.SeedConnection(t, e.db).ID

	var access struct {
		Items []adminapi.AccessDTO `json:"items"`
	}
	w := admin.send("GET", groupPath(viewers)+"/access", "", "")
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &access)
	if len(access.Items) != 3 {
		t.Fatalf("semua database harus tampil: %+v", access.Items)
	}
	for _, a := range access.Items {
		if a.Level != "none" || a.Name == "" || a.Engine == "" {
			t.Fatalf("akses awal salah: %+v", a)
		}
	}

	bad := []map[string]any{
		{"access": []map[string]any{{"db_id": db1, "level": "admin"}}},
		{"access": []map[string]any{{"db_id": 9999, "level": "view"}}},
		{"access": []map[string]any{{"db_id": db1, "level": "view"}, {"db_id": db1, "level": "edit"}}},
		{"access": []map[string]any{}},
		{"access": []map[string]any{{"db_id": db1, "level": "view"}, {"db_id": 9999, "level": "view"}}},
	}
	for _, body := range bad {
		expectAPIError(t, admin.json("PUT", groupPath(viewers)+"/access", body), http.StatusUnprocessableEntity, "validation_error")
	}
	if e.count("SELECT count(*) FROM group_db_access") != 0 {
		t.Fatal("perubahan akses tidak valid tetap tersimpan sebagian")
	}
	expectAPIError(t, admin.json("PUT", "/api/admin/groups/9999/access", map[string]any{"access": []map[string]any{{"db_id": db1, "level": "view"}}}), http.StatusNotFound, "not_found")

	w = admin.json("PUT", groupPath(viewers)+"/access", map[string]any{"access": []map[string]any{{"db_id": db1, "level": "view"}, {"db_id": db2, "level": "view"}}})
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &access)
	got := map[int64]string{}
	for _, a := range access.Items {
		got[a.DBID] = a.Level
	}
	if got[db1] != "view" || got[db2] != "view" || got[db3] != "none" {
		t.Fatalf("respons akses salah: %v", got)
	}
	expectStatus(t, admin.json("PUT", groupPath(editors)+"/access", map[string]any{"access": []map[string]any{{"db_id": db1, "level": "edit"}}}), http.StatusOK)

	a := e.apiLogin("alice", "password123")
	levels := levelsOf(t, a)
	if len(levels) != 2 || levels[db1] != "edit" || levels[db2] != "view" {
		t.Fatalf("akses efektif harus level tertinggi: %v", levels)
	}
	p := authz.Principal{UserID: alice.ID}
	if err := e.app.Authz.Can(ctx, p, db2, authz.EditMigration); err != authz.ErrForbidden {
		t.Fatalf("db 2 hanya View Only, got %v", err)
	}
	if err := e.app.Authz.Can(ctx, p, db3, authz.ViewDatabase); err != authz.ErrForbidden {
		t.Fatalf("db 3 tanpa akses, got %v", err)
	}

	expectStatus(t, admin.json("PUT", groupPath(editors)+"/access", map[string]any{"access": []map[string]any{{"db_id": db1, "level": "none"}}}), http.StatusOK)
	a = e.apiLogin("alice", "password123")
	if levelsOf(t, a)[db1] != "view" {
		t.Fatal("setelah akses edit dicabut, db 1 harus kembali ke view dari grup lain")
	}

	alicePath := userPath(alice.ID)
	expectStatus(t, admin.json("PATCH", alicePath, map[string]any{"is_active": false}), http.StatusOK)
	if err := e.app.Authz.Can(ctx, p, db1, authz.ViewDatabase); err != authz.ErrForbidden {
		t.Fatalf("user nonaktif tidak boleh punya akses, got %v", err)
	}
	expectRevoked(t, a, "dinonaktifkan")
	expectAPIError(t, e.api().login("alice", "password123"), http.StatusUnauthorized, "invalid_credentials")
}

func TestAPIAdminSecretsNeverLogged(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	c := e.apiLogin("admin", "password123")
	token := c.session()
	w := c.json("POST", "/api/admin/users", map[string]any{"username": "carol", "password": "rahasia-awal-xyz"})
	expectStatus(t, w, http.StatusCreated)
	var carol adminapi.UserDTO
	decodeBody(t, w, &carol)
	c.json("POST", "/api/admin/users", map[string]any{"username": "dodi", "password": "pendek1"})
	expectStatus(t, c.json("POST", userPath(carol.ID)+"/password", map[string]any{"password": "rahasia-baru-xyz"}), http.StatusNoContent)
	expectStatus(t, c.json("POST", "/api/logout", nil), http.StatusNoContent)
	logs := e.log.String()
	for _, secret := range []string{"password123", "rahasia-awal-xyz", "rahasia-baru-xyz", "pendek1", token, c.csrf} {
		if strings.Contains(logs, secret) {
			t.Fatalf("log memuat data rahasia %q", secret)
		}
	}
	if !strings.Contains(logs, "admin reset password") {
		t.Fatal("aksi admin harus tetap tercatat")
	}
}
