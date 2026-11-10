package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/adminapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approvalapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func (e *testEnv) pendingMigration(connID, requester int64) (int64, int64) {
	e.t.Helper()
	ctx := context.Background()
	var mid, eid int64
	if err := e.db.QueryRow(ctx, "INSERT INTO platform_migrations (database_id, title, created_by) VALUES ($1, 'Tambah kolom email', $2) RETURNING id", connID, requester).Scan(&mid); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "INSERT INTO platform_migration_revisions (migration_id, revision_number, sql_up, sql_down, checksum, created_by) VALUES ($1, 1, 'SELECT 1', 'SELECT 2', $2, $3)", mid, strings.Repeat("c", 64), requester); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.QueryRow(ctx, "INSERT INTO platform_migration_executions (migration_id, database_id, revision_number, kind, status, requested_by) VALUES ($1, $2, 1, 'up', 'pending_review', $3) RETURNING id", mid, connID, requester).Scan(&eid); err != nil {
		e.t.Fatal(err)
	}
	return mid, eid
}

func (e *testEnv) executionStatus(eid int64) string {
	e.t.Helper()
	var s string
	if err := e.db.QueryRow(context.Background(), "SELECT status FROM platform_migration_executions WHERE id = $1", eid).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *testEnv) setLeader(gid, userID int64) {
	e.t.Helper()
	if _, err := e.db.Exec(context.Background(), "UPDATE groups SET leader_id = $2 WHERE id = $1", gid, userID); err != nil {
		e.t.Fatal(err)
	}
}

func leaderPath(gid int64) string {
	return fmt.Sprintf("/api/admin/groups/%d/leader", gid)
}

func approvePath(mid int64, action string) string {
	return fmt.Sprintf("/api/migrations/%d/%s", mid, action)
}

func TestAPIGroupLeader(t *testing.T) {
	e := setup(t)
	e.createUser("admin", "password123", true)
	a := e.createUser("ani", "password123", false)
	b := e.createUser("budi", "password123", false)
	off := e.createUser("off", "password123", false)
	outsider := e.createUser("luar", "password123", false)
	gid := testutil.SeedGroup(t, e.db, "analis")
	for _, id := range []int64{a.ID, b.ID, off.ID} {
		testutil.SeedMember(t, e.db, gid, id)
	}
	e.app.Users.Update(context.Background(), off.ID, "off", false, false)
	admin := e.apiLogin("admin", "password123")

	expectInvalidField(t, admin.json("PUT", leaderPath(gid), map[string]any{"user_id": outsider.ID}), "user_id")
	expectInvalidField(t, admin.json("PUT", leaderPath(gid), map[string]any{"user_id": 9999}), "user_id")
	expectInvalidField(t, admin.json("PUT", leaderPath(gid), map[string]any{"user_id": off.ID}), "user_id")
	expectInvalidField(t, admin.json("PUT", leaderPath(gid), map[string]any{"user_id": "ani"}), "user_id")
	expectInvalidField(t, admin.send("PUT", leaderPath(gid), `{}`, admin.csrf), "user_id")
	expectAPIError(t, admin.json("PUT", leaderPath(9999), map[string]any{"user_id": a.ID}), http.StatusNotFound, "not_found")
	if e.count("SELECT count(*) FROM groups WHERE leader_id IS NOT NULL") != 0 {
		t.Fatal("leader tidak valid tersimpan")
	}

	ani := e.apiLogin("ani", "password123")
	budi := e.apiLogin("budi", "password123")
	expectAPIError(t, ani.json("PUT", leaderPath(gid), map[string]any{"user_id": a.ID}), http.StatusForbidden, "forbidden")

	w := admin.json("PUT", leaderPath(gid), map[string]any{"user_id": a.ID})
	expectStatus(t, w, http.StatusOK)
	var detail adminapi.GroupDetail
	decodeBody(t, w, &detail)
	if detail.Leader == nil || detail.Leader.ID != a.ID || detail.Leader.Username != "ani" {
		t.Fatalf("leader tidak tampil: %+v", detail.Leader)
	}
	expectRotated(t, ani, "jadi leader")
	expectStatus(t, budi.send("GET", "/api/me", "", ""), http.StatusOK)

	expectStatus(t, admin.json("PUT", leaderPath(gid), map[string]any{"user_id": b.ID}), http.StatusOK)
	expectRevoked(t, ani, "leader diganti")
	expectRotated(t, budi, "jadi leader")

	var page httpapi.Page[adminapi.GroupDTO]
	decodeBody(t, admin.send("GET", "/api/admin/groups", "", ""), &page)
	if page.Items[0].Leader == nil || page.Items[0].Leader.Username != "budi" {
		t.Fatalf("daftar grup tidak menampilkan leader: %+v", page.Items[0])
	}

	w = admin.json("PUT", leaderPath(gid), map[string]any{"user_id": nil})
	expectStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), `"leader":null`) {
		t.Fatalf("leader harus null: %s", w.Body.String())
	}
	expectRevoked(t, budi, "leader dilepas")

	expectStatus(t, admin.json("PUT", leaderPath(gid), map[string]any{"user_id": a.ID}), http.StatusOK)
	expectStatus(t, admin.send("DELETE", fmt.Sprintf("/api/admin/groups/%d/members/%d", gid, a.ID), "", admin.csrf), http.StatusNoContent)
	decodeBody(t, admin.send("GET", groupPath(gid), "", ""), &detail)
	if detail.Leader != nil {
		t.Fatal("leader yang dikeluarkan dari grup harus dilepas")
	}
}

func TestAPIApprovalFlow(t *testing.T) {
	e := setup(t)
	dev := e.createUser("dev", "password123", false)
	a := e.createUser("ani", "password123", false)
	b := e.createUser("budi", "password123", false)
	editor := e.createUser("edi", "password123", false)
	e.createUser("luar", "password123", false)
	conn := testutil.SeedConnection(t, e.db)
	g1 := testutil.SeedGroup(t, e.db, "analis")
	g2 := testutil.SeedGroup(t, e.db, "ops")
	g3 := testutil.SeedGroup(t, e.db, "pengaju")
	for _, id := range []int64{dev.ID, a.ID, editor.ID} {
		testutil.SeedMember(t, e.db, g1, id)
	}
	testutil.SeedMember(t, e.db, g2, b.ID)
	testutil.SeedMember(t, e.db, g3, dev.ID)
	testutil.SeedGrant(t, e.db, g1, conn.ID, authz.LevelEdit)
	testutil.SeedGrant(t, e.db, g2, conn.ID, authz.LevelView)
	testutil.SeedGrant(t, e.db, g3, conn.ID, authz.LevelEdit)
	e.setLeader(g1, a.ID)
	e.setLeader(g2, b.ID)
	e.setLeader(g3, dev.ID)
	mid, eid := e.pendingMigration(conn.ID, dev.ID)

	ani := e.apiLogin("ani", "password123")
	budi := e.apiLogin("budi", "password123")
	devc := e.apiLogin("dev", "password123")
	edi := e.apiLogin("edi", "password123")
	luar := e.apiLogin("luar", "password123")

	var page httpapi.Page[approvalapi.ApprovalDTO]
	w := ani.send("GET", "/api/approvals?mine=true", "", "")
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &page)
	if page.Total != 1 || page.Items[0].MigrationID != mid || !page.Items[0].CanDecide || page.Items[0].State != "blocked" || !strings.Contains(page.Items[0].Message, "Pengaju adalah leader Grup pengaju") || len(page.Items[0].Groups) != 3 {
		t.Fatalf("daftar persetujuan ani salah: %+v", page)
	}
	if page.Items[0].RequestedBy.Username != "dev" || page.Items[0].Database.ID != conn.ID || page.Items[0].Kind != "up" {
		t.Fatalf("isi item salah: %+v", page.Items[0])
	}
	for _, c := range []*apiClient{devc, edi, luar} {
		decodeBody(t, c.send("GET", "/api/approvals?mine=true", "", ""), &page)
		if page.Total != 0 {
			t.Fatalf("hanya leader yang belum memutuskan yang melihat daftar: %+v", page)
		}
	}
	decodeBody(t, edi.send("GET", "/api/approvals", "", ""), &page)
	if page.Total != 1 || page.Items[0].CanDecide {
		t.Fatalf("anggota dengan akses melihat permintaan tanpa hak memutuskan: %+v", page)
	}
	decodeBody(t, luar.send("GET", "/api/approvals", "", ""), &page)
	if page.Total != 0 {
		t.Fatal("orang tanpa akses tidak boleh melihat permintaan")
	}
	expectInvalidField(t, ani.send("GET", "/api/approvals?mine=ya", "", ""), "mine")

	expectInvalidField(t, ani.json("POST", approvePath(mid, "approve"), map[string]any{}), "revision_number")
	expectInvalidField(t, ani.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1, "reason": "x"}), "reason")
	expectAPIError(t, ani.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 2}), http.StatusConflict, "revision_changed")
	expectAPIError(t, edi.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1}), http.StatusForbidden, "forbidden")
	expectAPIError(t, luar.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1}), http.StatusForbidden, "forbidden")
	expectAPIError(t, devc.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1}), http.StatusForbidden, "self_approval")
	expectAPIError(t, ani.send("POST", approvePath(mid, "approve"), `{"revision_number":1}`, ""), http.StatusForbidden, "csrf_invalid")
	expectAPIError(t, e.api().send("POST", approvePath(mid, "approve"), `{"revision_number":1}`, ""), http.StatusForbidden, "csrf_invalid")
	anon := e.api()
	anon.fetchCSRF()
	expectAPIError(t, anon.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1}), http.StatusUnauthorized, "unauthenticated")
	expectAPIError(t, ani.json("POST", approvePath(9999, "approve"), map[string]any{"revision_number": 1}), http.StatusNotFound, "not_found")
	if e.count("SELECT count(*) FROM migration_approvals") != 0 {
		t.Fatal("keputusan tidak sah tersimpan")
	}

	w = ani.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1})
	expectStatus(t, w, http.StatusOK)
	var st approvalapi.ApprovalDTO
	decodeBody(t, w, &st)
	if st.State != "blocked" || !strings.Contains(st.Message, "pengaju") || st.CanDecide {
		t.Fatalf("pengaju memimpin grup wajib, jadi harus tertahan: %+v", st)
	}
	expectAPIError(t, ani.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1}), http.StatusConflict, "already_decided")

	e.createUser("admin", "password123", true)
	adm := e.apiLogin("admin", "password123")
	testutil.SeedMember(t, e.db, g3, editor.ID)
	expectStatus(t, adm.json("PUT", leaderPath(g3), map[string]any{"user_id": editor.ID}), http.StatusOK)
	expectRevoked(t, devc, "jabatan leader dicabut")
	devc = e.apiLogin("dev", "password123")
	edi = e.apiLogin("edi", "password123")
	expectStatus(t, edi.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1}), http.StatusOK)

	w = budi.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1})
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &st)
	if st.State != "approved" || st.ExecutionStatus != "approved" || e.executionStatus(eid) != "approved" {
		t.Fatalf("semua leader setuju: %+v", st)
	}
	for _, g := range st.Groups {
		if g.Decision != "approved" || g.DecidedBy == nil || g.DecidedAt == nil {
			t.Fatalf("keputusan grup tidak lengkap: %+v", g)
		}
	}
	expectAPIError(t, budi.json("POST", approvePath(mid, "approve"), map[string]any{"revision_number": 1}), http.StatusConflict, "no_pending_review")

	w = devc.send("GET", approvePath(mid, "approvals"), "", "")
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &st)
	if st.State != "approved" || st.ExecutionID != eid {
		t.Fatalf("status untuk pengaju: %+v", st)
	}
	expectAPIError(t, luar.send("GET", approvePath(mid, "approvals"), "", ""), http.StatusForbidden, "forbidden")
	expectAPIError(t, luar.send("GET", approvePath(9999, "approvals"), "", ""), http.StatusNotFound, "not_found")
	if err := e.app.Approvals.Check(context.Background(), eid); err != nil {
		t.Fatalf("Check setelah semua setuju: %v", err)
	}
}

func TestAPIRejectRequiresReason(t *testing.T) {
	e := setup(t)
	dev := e.createUser("dev", "password123", false)
	a := e.createUser("ani", "password123", false)
	conn := testutil.SeedConnection(t, e.db)
	gid := testutil.SeedGroup(t, e.db, "analis")
	testutil.SeedMember(t, e.db, gid, dev.ID)
	testutil.SeedMember(t, e.db, gid, a.ID)
	testutil.SeedGrant(t, e.db, gid, conn.ID, authz.LevelEdit)
	e.setLeader(gid, a.ID)
	mid, eid := e.pendingMigration(conn.ID, dev.ID)
	ani := e.apiLogin("ani", "password123")

	expectInvalidField(t, ani.json("POST", approvePath(mid, "reject"), map[string]any{"revision_number": 1}), "reason")
	expectInvalidField(t, ani.json("POST", approvePath(mid, "reject"), map[string]any{"revision_number": 1, "reason": "  "}), "reason")
	expectInvalidField(t, ani.json("POST", approvePath(mid, "reject"), map[string]any{"revision_number": 1, "reason": strings.Repeat("x", 501)}), "reason")
	w := ani.json("POST", approvePath(mid, "reject"), map[string]any{"revision_number": 1, "reason": "Kolom email harus unik."})
	expectStatus(t, w, http.StatusOK)
	var st approvalapi.ApprovalDTO
	decodeBody(t, w, &st)
	if st.State != "rejected" || st.ExecutionStatus != "rejected" || st.Groups[0].Reason != "Kolom email harus unik." || e.executionStatus(eid) != "rejected" {
		t.Fatalf("penolakan salah: %+v", st)
	}
	if !strings.Contains(e.log.String(), "migration decision") || strings.Contains(e.log.String(), "password123") {
		t.Fatal("keputusan harus tercatat tanpa rahasia")
	}
}

func TestAPIRequiredApprovers(t *testing.T) {
	e := setup(t)
	dev := e.createUser("dev", "password123", false)
	a := e.createUser("ani", "password123", false)
	e.createUser("luar", "password123", false)
	e.createUser("admin", "password123", true)
	conn := testutil.SeedConnection(t, e.db)
	other := testutil.SeedConnection(t, e.db)
	g1 := testutil.SeedGroup(t, e.db, "analis")
	g2 := testutil.SeedGroup(t, e.db, "ops")
	testutil.SeedMember(t, e.db, g1, dev.ID)
	testutil.SeedMember(t, e.db, g1, a.ID)
	testutil.SeedGrant(t, e.db, g1, conn.ID, authz.LevelEdit)
	testutil.SeedGrant(t, e.db, g2, conn.ID, authz.LevelView)
	e.setLeader(g1, a.ID)
	devc := e.apiLogin("dev", "password123")
	path := fmt.Sprintf("/api/databases/%d/approvers", conn.ID)

	w := devc.send("GET", path, "", "")
	expectStatus(t, w, http.StatusOK)
	var out approvalapi.RequiredApproversDTO
	decodeBody(t, w, &out)
	if out.Ready || !strings.Contains(out.Message, "Grup ops belum punya leader aktif") || len(out.Groups) != 2 || out.Groups[0].Leader == nil || out.Groups[0].Leader.Username != "ani" || out.Groups[1].Leader != nil {
		t.Fatalf("approver wajib salah: %+v", out)
	}
	testutil.SeedMember(t, e.db, g2, dev.ID)
	e.setLeader(g2, dev.ID)
	decodeBody(t, devc.send("GET", path, "", ""), &out)
	if !out.Ready || out.Groups[1].Leader == nil {
		t.Fatalf("semua grup punya leader: %+v", out)
	}
	expectAPIError(t, devc.send("GET", fmt.Sprintf("/api/databases/%d/approvers", other.ID), "", ""), http.StatusForbidden, "forbidden")
	expectAPIError(t, e.apiLogin("luar", "password123").send("GET", path, "", ""), http.StatusForbidden, "forbidden")
	expectAPIError(t, e.apiLogin("admin", "password123").send("GET", path, "", ""), http.StatusForbidden, "forbidden")
	expectAPIError(t, devc.send("GET", "/api/databases/abc/approvers", "", ""), http.StatusNotFound, "not_found")
	expectAPIError(t, e.api().send("GET", path, "", ""), http.StatusUnauthorized, "unauthenticated")
}
