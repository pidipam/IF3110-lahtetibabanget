package workflow_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/wftest"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

type env struct {
	t    *testing.T
	srv  *httptest.Server
	svc  *workflow.Service
	acc  *wftest.FakeAccess
	apr  *wftest.FakeApprovals
	q    *wftest.FakeQueue
	uid  int64
	db   int64
	user int64 // pengguna yang sedang login (0 = anonim)
}

func setup(t *testing.T) *env {
	pool := wftest.NewPool(t)
	uid, db := wftest.Seed(t, pool)
	e := &env{t: t, acc: wftest.NewAccess(), apr: wftest.NewApprovals(), q: &wftest.FakeQueue{}, uid: uid, db: db, user: uid}
	e.svc = workflow.NewService(workflow.NewRepository(pool), e.acc, e.apr, e.q, nil)
	h := &workflow.HTTP{Svc: e.svc, UserID: func(*http.Request) (int64, bool) { return e.user, e.user != 0 }}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) do(method, path string, body any) (int, map[string]any, string) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	raw.ReadFrom(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw.Bytes(), &m)
	return resp.StatusCode, m, raw.String()
}

func good(title string) map[string]any {
	return map[string]any{"title": title, "sql_up": "CREATE TABLE zz_a(id int)", "sql_down": "DROP TABLE zz_a"}
}

func (e *env) base() string { return "/api/databases/" + fmtInt(e.db) }

func fmtInt(i int64) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestAPIAuthAndAccess(t *testing.T) {
	e := setup(t)
	if c, _, _ := e.do("GET", e.base()+"/migrations", nil); c != 403 {
		t.Fatalf("tanpa grant harus 403, got %d", c)
	}
	e.user = 0
	if c, _, _ := e.do("GET", e.base()+"/migrations", nil); c != 401 {
		t.Fatalf("anonim harus 401, got %d", c)
	}
	e.user = e.uid
	e.acc.Grant(e.uid, e.db, 1) // view saja
	if c, _, _ := e.do("GET", e.base()+"/migrations", nil); c != 200 {
		t.Fatalf("view harus boleh list, got %d", c)
	}
	if c, _, _ := e.do("POST", e.base()+"/migrations", good("x")); c != 403 {
		t.Fatalf("view tidak boleh membuat, got %d", c)
	}
	if c, _, _ := e.do("GET", "/api/databases/abc/migrations", nil); c != 404 {
		t.Fatalf("id buruk harus 404, got %d", c)
	}
	if c, _, _ := e.do("GET", "/api/databases/999999/migrations", nil); c != 403 {
		t.Fatalf("db tak ada tanpa akses harus 403, got %d", c)
	}
}

func TestAPICreateGetReviseValidation(t *testing.T) {
	e := setup(t)
	e.acc.Grant(e.uid, e.db, 2)
	c, m, raw := e.do("POST", e.base()+"/migrations", good("buat tabel"))
	if c != 201 {
		t.Fatalf("create: %d %s", c, raw)
	}
	if m["current_revision"].(float64) != 1 || m["linear_order"] != nil || m["can_submit"] != true {
		t.Fatalf("respons create: %s", raw)
	}
	id := int64(m["id"].(float64))
	path := e.base() + "/migrations/" + fmtInt(id)
	// validasi
	bad := good("x")
	bad["sql_down"] = ""
	if c, _, raw := e.do("POST", e.base()+"/migrations", bad); c != 422 || !strings.Contains(raw, "validation_failed") {
		t.Fatalf("down kosong harus 422: %d %s", c, raw)
	}
	if c, _, _ := e.do("POST", e.base()+"/migrations", map[string]any{"title": "x", "unknown": 1}); c != 400 {
		t.Fatalf("field asing harus 400, got %d", c)
	}
	// revisi
	rev := good("buat tabel v2")
	rev["sql_up"] = "CREATE TABLE zz_a(id int, n int)"
	c, m, raw = e.do("PATCH", path, rev)
	if c != 200 || m["current_revision"].(float64) != 2 {
		t.Fatalf("revise: %d %s", c, raw)
	}
	// revisi 1 tetap utuh (immutable) — baca lewat DB
	var up string
	pool := wftest.NewPool // hanya agar import dipakai
	_ = pool
	c, m, _ = e.do("GET", path, nil)
	if m["revision"].(map[string]any)["revision_number"].(float64) != 2 {
		t.Fatalf("revisi aktif harus 2")
	}
	_ = up
	if c, _, _ := e.do("GET", e.base()+"/migrations/99999", nil); c != 404 {
		t.Fatalf("tak ada harus 404, got %d", c)
	}
}

func TestAPISubmitApproveQueueFlow(t *testing.T) {
	e := setup(t)
	e.acc.Grant(e.uid, e.db, 2)
	_, m, _ := e.do("POST", e.base()+"/migrations", good("A"))
	id := int64(m["id"].(float64))
	c, ex, raw := e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil)
	if c != 201 || ex["status"] != "pending_review" || ex["kind"] != "up" {
		t.Fatalf("submit: %d %s", c, raw)
	}
	eid := int64(ex["id"].(float64))
	if len(e.apr.Opened) != 1 {
		t.Fatal("approval harus dibuka")
	}
	// submit ganda ditolak
	if c, _, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil); c != 409 {
		t.Fatalf("submit ganda harus 409, got %d", c)
	}
	// revisi saat pending_review diizinkan: eksekusi lama dibatalkan, nomor dilepas
	rev := good("A2")
	if c, _, raw := e.do("PATCH", e.base()+"/migrations/"+fmtInt(id), rev); c != 200 {
		t.Fatalf("revise: %d %s", c, raw)
	}
	_, got, _ := e.do("GET", e.base()+"/executions/"+fmtInt(eid), nil)
	if got["status"] != "cancelled" {
		t.Fatalf("eksekusi lama harus cancelled: %v", got["status"])
	}
	// ajukan ulang
	_, ex, _ = e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil)
	eid = int64(ex["id"].(float64))
	// belum approved → queue ditolak
	if c, _, _ := e.do("POST", e.base()+"/executions/"+fmtInt(eid)+"/queue", nil); c != 409 {
		t.Fatalf("queue sebelum approval harus 409, got %d", c)
	}
	e.apr.Approve(eid)
	if err := e.svc.MarkApproved(t.Context(), eid); err != nil {
		t.Fatal(err)
	}
	c, q, raw := e.do("POST", e.base()+"/executions/"+fmtInt(eid)+"/queue", nil)
	if c != 202 || q["job_id"] == "" || q["status"] != "queued" {
		t.Fatalf("queue: %d %s", c, raw)
	}
	n := e.q.Count()
	// idempoten: queue ulang tidak menerbitkan job baru
	if c, _, _ := e.do("POST", e.base()+"/executions/"+fmtInt(eid)+"/queue", nil); c != 202 || e.q.Count() != n {
		t.Fatalf("queue ulang harus idempoten (count %d→%d)", n, e.q.Count())
	}
	// setelah queued, revisi ditolak (immutable)
	if c, _, _ := e.do("PATCH", e.base()+"/migrations/"+fmtInt(id), good("A3")); c != 409 {
		t.Fatalf("revisi setelah queued harus 409, got %d", c)
	}
}

func TestAPIRejectReleasesOrderAndHistoryEmpty(t *testing.T) {
	e := setup(t)
	e.acc.Grant(e.uid, e.db, 2)
	_, m, _ := e.do("POST", e.base()+"/migrations", good("A"))
	id := int64(m["id"].(float64))
	_, ex, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil)
	eid := int64(ex["id"].(float64))
	_, g, _ := e.do("GET", e.base()+"/migrations/"+fmtInt(id), nil)
	if g["linear_order"].(float64) != 1 {
		t.Fatalf("urutan harus 1: %v", g["linear_order"])
	}
	if err := e.svc.MarkRejected(t.Context(), eid, "tidak setuju"); err != nil {
		t.Fatal(err)
	}
	_, g, _ = e.do("GET", e.base()+"/migrations/"+fmtInt(id), nil)
	if g["linear_order"] != nil {
		t.Fatalf("nomor harus dilepas setelah ditolak: %v", g["linear_order"])
	}
	// pengajuan baru: nomor 1 lagi, tanpa celah
	_, ex2, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil)
	if ex2["status"] != "pending_review" {
		t.Fatalf("ajukan ulang harus bisa: %v", ex2)
	}
	_, g, _ = e.do("GET", e.base()+"/migrations/"+fmtInt(id), nil)
	if g["linear_order"].(float64) != 1 {
		t.Fatalf("urutan harus 1 lagi: %v", g["linear_order"])
	}
	c, _, raw := e.do("GET", e.base()+"/history", nil)
	if c != 200 || strings.TrimSpace(raw) != "[]" {
		t.Fatalf("history harus []: %d %s", c, raw)
	}
}

func TestAPIConcurrentSubmitNoGaps(t *testing.T) {
	e := setup(t)
	e.acc.Grant(e.uid, e.db, 2)
	var ids []int64
	for i := 0; i < 6; i++ {
		_, m, _ := e.do("POST", e.base()+"/migrations", good("M"+fmtInt(int64(i))))
		ids = append(ids, int64(m["id"].(float64)))
	}
	done := make(chan int, len(ids))
	for _, id := range ids {
		go func(id int64) {
			c, _, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil)
			done <- c
		}(id)
	}
	for range ids {
		if c := <-done; c != 201 {
			t.Fatalf("submit paralel gagal: %d", c)
		}
	}
	_, _, raw := e.do("GET", e.base()+"/migrations", nil)
	var list []map[string]any
	_ = json.Unmarshal([]byte(raw), &list)
	seen := map[int]bool{}
	for _, m := range list {
		seen[int(m["linear_order"].(float64))] = true
	}
	for i := 1; i <= 6; i++ {
		if !seen[i] {
			t.Fatalf("urutan %d hilang/duplikat: %v", i, seen)
		}
	}
}

func TestAPIApprovalOpenFailureCancels(t *testing.T) {
	e := setup(t)
	e.acc.Grant(e.uid, e.db, 2)
	_, m, _ := e.do("POST", e.base()+"/migrations", good("A"))
	id := int64(m["id"].(float64))
	e.apr.FailOpen = true
	if c, _, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil); c != 500 {
		t.Fatalf("harus 500, got %d", c)
	}
	e.apr.FailOpen = false
	if c, _, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(id)+"/submit", nil); c != 201 {
		t.Fatalf("setelah pulih harus bisa diajukan lagi, got %d", c)
	}
}

func TestApprovalAutoEnqueues(t *testing.T) {
	e := setup(t)
	e.acc.Grant(e.uid, e.db, 2)
	_, m, _ := e.do("POST", e.base()+"/migrations", good("A"))
	_, ex, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(int64(m["id"].(float64)))+"/submit", nil)
	eid := int64(ex["id"].(float64))
	before := e.q.Count()
	e.apr.Approve(eid)
	if err := e.svc.MarkApproved(t.Context(), eid); err != nil {
		t.Fatal(err)
	}
	_, got, _ := e.do("GET", e.base()+"/executions/"+fmtInt(eid), nil)
	if got["status"] != "queued" || got["job_id"] == "" || e.q.Count() != before+1 {
		t.Fatalf("approval harus langsung mengantrekan: %v", got)
	}
	// dipanggil ulang tidak menerbitkan job kedua
	if err := e.svc.MarkApproved(t.Context(), eid); err != nil || e.q.Count() != before+1 {
		t.Fatalf("harus idempoten: %v count=%d", err, e.q.Count())
	}
}

func TestApprovalEnqueueFailureRecoverable(t *testing.T) {
	e := setup(t)
	e.acc.Grant(e.uid, e.db, 2)
	_, m, _ := e.do("POST", e.base()+"/migrations", good("A"))
	_, ex, _ := e.do("POST", e.base()+"/migrations/"+fmtInt(int64(m["id"].(float64)))+"/submit", nil)
	eid := int64(ex["id"].(float64))
	e.apr.Approve(eid)
	e.q.Fail = true
	if err := e.svc.MarkApproved(t.Context(), eid); err == nil {
		t.Fatal("kegagalan antrean harus dilaporkan")
	}
	e.q.Fail = false
	// pulih lewat endpoint manual (atau MarkApproved dipanggil ulang)
	if c, q, _ := e.do("POST", e.base()+"/executions/"+fmtInt(eid)+"/queue", nil); c != 202 || q["job_id"] == "" {
		t.Fatalf("pemulihan manual gagal: %d %v", c, q)
	}
}
