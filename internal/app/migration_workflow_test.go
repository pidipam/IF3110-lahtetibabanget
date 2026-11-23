package app

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

type spaMigration struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	LinearOrder     int    `json:"linear_order"`
	CurrentRevision struct {
		RevisionNumber int `json:"revision_number"`
	} `json:"current_revision"`
}

type spaJob struct {
	JobID          string   `json:"job_id"`
	Status         string   `json:"status"`
	Kind           string   `json:"kind"`
	ErrorMessage   string   `json:"error_message"`
	AllowedActions []string `json:"allowed_actions"`
}

type spaHistory struct {
	Items []struct {
		ExecutionID         string `json:"execution_id"`
		ExecutionType       string `json:"execution_type"`
		ExecutedBy          string `json:"executed_by"`
		RevertedExecutionID string `json:"reverted_execution_id"`
	} `json:"items"`
	Total int `json:"total"`
}

func waitJob(t *testing.T, c *apiClient, id string, want ...string) spaJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		w := c.json("GET", "/api/jobs/"+id, nil)
		expectStatus(t, w, http.StatusOK)
		var j spaJob
		decodeBody(t, w, &j)
		for _, s := range want {
			if j.Status == s {
				return j
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s berstatus %s (%s), menunggu %v", id, j.Status, j.ErrorMessage, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Alur penuh lewat kontrak SPA: buat -> ajukan -> leader menyetujui -> jalankan ->
// worker -> rollback (butuh approval lagi) -> riwayat dengan filter Hide rollback.
// Database target = database uji itu sendiri, jadi tidak butuh target-db :5435.
func TestWorkflowSPAEndToEnd(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(testutil.RequireEnv(t, testutil.EnvTestDB))
	if err != nil {
		t.Fatal(err)
	}
	conn := testutil.SeedConnection(t, e.db, func(in *dbconn.Input) {
		in.Host, in.Port, in.DBName, in.Username, in.Password = cfg.Host, int(cfg.Port), cfg.Database, cfg.User, cfg.Password
	})
	t.Cleanup(func() { e.db.Exec(context.Background(), "DROP TABLE IF EXISTS zz_wf_api") })
	editor := e.createUser("wf_editor", "password123", false)
	viewer := e.createUser("wf_viewer", "password123", false)
	leader := e.createUser("wf_leader", "password123", false)
	e.createUser("wf_outsider", "password123", false)
	gid := testutil.SeedGroup(t, e.db, "wf-group")
	testutil.SeedMember(t, e.db, gid, editor.ID)
	testutil.SeedMember(t, e.db, gid, leader.ID)
	gv := testutil.SeedGroup(t, e.db, "wf-viewers")
	testutil.SeedMember(t, e.db, gv, viewer.ID)
	testutil.SeedGrant(t, e.db, gid, conn.ID, 2)
	testutil.SeedGrant(t, e.db, gv, conn.ID, 1)
	e.setLeader(gid, leader.ID)
	e.setLeader(gv, viewer.ID) // setiap grup yang punya akses wajib menyetujui
	_ = ctx
	base := fmt.Sprintf("/api/databases/%d", conn.ID)
	body := map[string]any{"title": "tabel api", "sql_up": "CREATE TABLE zz_wf_api (id int)", "sql_down": "DROP TABLE zz_wf_api",
		"is_irreversible": false, "is_batch_mode": false}

	// akses
	anon := e.api()
	expectAPIError(t, anon.json("GET", base+"/migrations", nil), http.StatusUnauthorized, "unauthenticated")
	expectStatus(t, e.apiLogin("wf_outsider", "password123").json("GET", base+"/migrations", nil), http.StatusForbidden)
	vw := e.apiLogin("wf_viewer", "password123")
	expectStatus(t, vw.json("GET", base+"/migrations", nil), http.StatusOK)
	expectStatus(t, vw.json("POST", base+"/migrations", body), http.StatusForbidden)

	ed := e.apiLogin("wf_editor", "password123")
	expectStatus(t, ed.send("POST", base+"/migrations", `{}`, ""), http.StatusForbidden) // tanpa CSRF
	expectInvalidField(t, ed.json("POST", base+"/migrations", map[string]any{"title": "x", "sql_up": "BEGIN; SELECT 1", "sql_down": "SELECT 1"}), "sql_up")

	// buat -> ajukan
	w := ed.json("POST", base+"/migrations", body)
	expectStatus(t, w, http.StatusCreated)
	var m spaMigration
	decodeBody(t, w, &m)
	if m.Status != "Draft" {
		t.Fatalf("status awal %q", m.Status)
	}
	mp := "/api/migrations/" + m.ID
	expectAPIError(t, ed.json("POST", mp+"/execute", nil), http.StatusConflict, "invalid_status") // belum disetujui
	w = ed.json("POST", mp+"/submit", nil)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &m)
	if m.Status != "Pending Review" || m.LinearOrder != 1 {
		t.Fatalf("setelah submit: %+v", m)
	}

	// pengaju tidak boleh menyetujui sendiri; leader boleh
	expectStatus(t, ed.json("POST", mp+"/approve", map[string]any{"revision_number": 1}), http.StatusForbidden)
	ld := e.apiLogin("wf_leader", "password123")
	approveAll := func() {
		t.Helper()
		for _, c := range []*apiClient{ld, vw} {
			expectStatus(t, c.json("POST", mp+"/approve", map[string]any{"revision_number": 1}), http.StatusOK)
		}
	}
	approveAll()
	w = ed.json("GET", mp, nil)
	decodeBody(t, w, &m)
	if m.Status != "Approved" {
		t.Fatalf("setelah approve: %q", m.Status)
	}

	// jalankan -> worker
	w = ed.json("POST", mp+"/execute", nil)
	expectStatus(t, w, http.StatusAccepted)
	var ex struct {
		JobID string `json:"job_id"`
	}
	decodeBody(t, w, &ex)
	j := waitJob(t, ed, ex.JobID, "success", "failed")
	if j.Status != "success" {
		t.Fatalf("job: %+v", j)
	}
	if e.count("SELECT count(*) FROM information_schema.tables WHERE table_name='zz_wf_api'") != 1 {
		t.Fatal("tabel target tidak terbentuk")
	}
	if len(j.AllowedActions) != 1 || j.AllowedActions[0] != "rollback" {
		t.Fatalf("aksi job sukses: %v", j.AllowedActions)
	}
	expectStatus(t, vw.json("POST", "/api/jobs/"+ex.JobID+"/rollback", nil), http.StatusForbidden)

	// rollback: butuh approval lagi, lalu dijalankan
	w = ed.json("POST", "/api/jobs/"+ex.JobID+"/rollback", nil)
	expectStatus(t, w, http.StatusOK)
	var act struct {
		NeedsApproval bool `json:"needs_approval"`
	}
	decodeBody(t, w, &act)
	if !act.NeedsApproval {
		t.Fatal("rollback harus butuh approval")
	}
	approveAll()
	w = ed.json("POST", mp+"/execute", nil)
	expectStatus(t, w, http.StatusAccepted)
	decodeBody(t, w, &ex)
	if rb := waitJob(t, ed, ex.JobID, "success", "failed"); rb.Status != "success" || rb.Kind != "rollback" {
		t.Fatalf("rollback job: %+v", rb)
	}
	if e.count("SELECT count(*) FROM information_schema.tables WHERE table_name='zz_wf_api'") != 0 {
		t.Fatal("tabel target masih ada setelah rollback")
	}

	// riwayat: semua = 2, Hide rollback = 0 (pasangan up+rollback disembunyikan)
	var all, hidden spaHistory
	decodeBody(t, ed.json("GET", base+"/history", nil), &all)
	decodeBody(t, ed.json("GET", base+"/history?hide_rollback=true&page=1&page_size=10", nil), &hidden)
	if all.Total != 2 || all.Items[0].ExecutionType != "ROLLBACK" || all.Items[0].ExecutedBy != "wf_editor" || hidden.Total != 0 {
		t.Fatalf("riwayat: semua=%+v tersembunyi=%+v", all, hidden)
	}
	expectStatus(t, ed.json("GET", base+"/history/"+all.Items[0].ExecutionID, nil), http.StatusOK)
	expectStatus(t, ed.json("GET", base+"/history/99999", nil), http.StatusNotFound)
}

// Penolakan melepas nomor urut sehingga migration berikutnya tidak melompat.
func TestWorkflowRejectReleasesOrder(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	editor := e.createUser("rj_editor", "password123", false)
	leader := e.createUser("rj_leader", "password123", false)
	gid := testutil.SeedGroup(t, e.db, "rj-group")
	testutil.SeedMember(t, e.db, gid, editor.ID)
	testutil.SeedMember(t, e.db, gid, leader.ID)
	testutil.SeedGrant(t, e.db, gid, conn.ID, 2)
	e.setLeader(gid, leader.ID)
	base := fmt.Sprintf("/api/databases/%d/migrations", conn.ID)
	body := map[string]any{"title": "m", "sql_up": "SELECT 1", "sql_down": "SELECT 2", "is_irreversible": false, "is_batch_mode": false}
	ed, ld := e.apiLogin("rj_editor", "password123"), e.apiLogin("rj_leader", "password123")
	var m spaMigration
	decodeBody(t, ed.json("POST", base, body), &m)
	expectStatus(t, ed.json("POST", "/api/migrations/"+m.ID+"/submit", nil), http.StatusOK)
	expectStatus(t, ld.json("POST", "/api/migrations/"+m.ID+"/reject", map[string]any{"revision_number": 1, "reason": "belum siap"}), http.StatusOK)
	var got spaMigration
	decodeBody(t, ed.json("GET", "/api/migrations/"+m.ID, nil), &got)
	if got.Status != "Draft" || got.LinearOrder != 0 {
		t.Fatalf("setelah ditolak: %+v", got)
	}
}
