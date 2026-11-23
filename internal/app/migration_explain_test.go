package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

type explainNode struct {
	Node string `json:"node"`
	Relation string `json:"relation"`
	Join string `json:"join"`
	HasActual bool `json:"has_actual"`
}

type explainResult struct {
	Analyze bool `json:"analyze"`
	RevisionNumber int `json:"revision_number"`
	Statements []struct {
		Number int `json:"number"`
		NoPlan bool `json:"no_plan"`
		Error string `json:"error"`
		Nodes []explainNode `json:"nodes"`
		HasTiming bool `json:"has_timing"`
		ExecutionTime float64 `json:"execution_time"`
	} `json:"statements"`
}

func TestMigrationExplainAPI(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	conn := testutil.SeedConnection(t, e.db)
	editor := e.apiMember("editor", conn.ID, authz.LevelEdit)
	viewer := e.apiMember("viewer", conn.ID, authz.LevelView)
	target, err := pgxpool.New(ctx, testutil.RequireEnv(t, "TARGET_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.Close)
	changed := func() int {
		var n int
		if err := target.QueryRow(ctx, "SELECT count(*) FROM customers WHERE name = 'zz-diubah-explain'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	body := map[string]any{"title": "analisis", "sql_up": "UPDATE customers SET name = 'zz-diubah-explain'; SELECT c.name FROM customers c JOIN orders o ON o.customer_id = c.id;", "sql_down": "SELECT 1", "is_irreversible": false, "is_batch_mode": false}
	w := editor.json("POST", fmt.Sprintf("/api/databases/%d/migrations", conn.ID), body)
	expectStatus(t, w, http.StatusCreated)
	var m spaMigration
	decodeBody(t, w, &m)
	path := "/api/migrations/" + m.ID + "/explain"

	w = editor.json("POST", path, map[string]any{"analyze": false})
	expectStatus(t, w, http.StatusOK)
	var plan explainResult
	decodeBody(t, w, &plan)
	if plan.Analyze || plan.RevisionNumber != 1 || len(plan.Statements) != 2 {
		t.Fatalf("hasil EXPLAIN salah: %+v", plan)
	}
	first, second := plan.Statements[0], plan.Statements[1]
	if len(first.Nodes) == 0 || first.Nodes[0].Node != "ModifyTable (Update)" || first.Nodes[0].Relation != "customers" || first.Nodes[0].HasActual {
		t.Fatalf("plan UPDATE salah: %+v", first.Nodes)
	}
	joined := false
	for _, n := range second.Nodes {
		joined = joined || n.Join != ""
	}
	if !joined {
		t.Fatalf("plan SELECT harus memuat join: %+v", second.Nodes)
	}

	w = editor.json("POST", path, map[string]any{"analyze": true})
	expectStatus(t, w, http.StatusOK)
	var an explainResult
	decodeBody(t, w, &an)
	if !an.Analyze || !an.Statements[0].Nodes[0].HasActual || !an.Statements[0].HasTiming || an.Statements[0].ExecutionTime <= 0 {
		t.Fatalf("EXPLAIN ANALYZE tanpa data aktual: %+v", an.Statements[0])
	}
	if changed() != 0 {
		t.Fatal("EXPLAIN ANALYZE mengubah data di database target")
	}
	var got spaMigration
	decodeBody(t, editor.json("GET", "/api/migrations/"+m.ID, nil), &got)
	if got.Status != "Draft" {
		t.Fatalf("EXPLAIN tidak boleh mengubah status migration: %q", got.Status)
	}

	expectStatus(t, editor.send("POST", path, `{"analyze":true}`, ""), http.StatusForbidden)
	expectAPIError(t, viewer.json("POST", path, map[string]any{"analyze": true}), http.StatusForbidden, "forbidden")
	expectAPIError(t, e.apiUser("outsider", false).json("POST", path, map[string]any{"analyze": true}), http.StatusForbidden, "forbidden")
	anon := e.api()
	anon.fetchCSRF()
	expectAPIError(t, anon.json("POST", path, map[string]any{"analyze": true}), http.StatusUnauthorized, "unauthenticated")
	expectAPIError(t, editor.json("POST", "/api/migrations/999999/explain", map[string]any{"analyze": true}), http.StatusNotFound, "not_found")
	expectAPIError(t, editor.json("POST", "/api/migrations/abc/explain", map[string]any{"analyze": true}), http.StatusNotFound, "not_found")
	expectAPIError(t, editor.json("POST", path, map[string]any{"mode": "x"}), http.StatusUnprocessableEntity, "validation_error")
	if changed() != 0 {
		t.Fatal("permintaan yang ditolak menjalankan EXPLAIN ANALYZE")
	}

	expectStatus(t, editor.json("POST", "/api/migrations/"+m.ID+"/submit", nil), http.StatusOK)
	expectStatus(t, editor.json("POST", path, map[string]any{"analyze": false}), http.StatusOK)
	if _, err := e.db.Exec(ctx, "UPDATE platform_migration_executions SET status = 'failed' WHERE migration_id = $1", m.ID); err != nil {
		t.Fatal(err)
	}
	expectAPIError(t, editor.json("POST", path, map[string]any{"analyze": false}), http.StatusConflict, "invalid_status")
}

func TestMigrationExplainTargetDown(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db, func(in *dbconn.Input) { in.Port = 1 })
	editor := e.apiMember("editor", conn.ID, authz.LevelEdit)
	body := map[string]any{"title": "target mati", "sql_up": "SELECT 1", "sql_down": "SELECT 1", "is_irreversible": false, "is_batch_mode": false}
	w := editor.json("POST", fmt.Sprintf("/api/databases/%d/migrations", conn.ID), body)
	expectStatus(t, w, http.StatusCreated)
	var m spaMigration
	decodeBody(t, w, &m)
	w = editor.json("POST", "/api/migrations/"+m.ID+"/explain", map[string]any{"analyze": false})
	expectAPIError(t, w, http.StatusServiceUnavailable, "target_unavailable")
	if strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Fatal("pesan galat membocorkan host")
	}
}
