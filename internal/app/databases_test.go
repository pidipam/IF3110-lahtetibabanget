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

type dbItem struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Engine string `json:"engine"`
	Level  string `json:"level"`
}

type dbListResponse struct {
	Items    []dbItem `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int      `json:"total"`
}

type apiColumn struct {
	Position   int    `json:"position"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	HasDefault bool   `json:"has_default"`
}

type apiConstraint struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Definition string   `json:"definition"`
	Columns    []string `json:"columns"`
}

type apiIndex struct {
	Name       string `json:"name"`
	Unique     bool   `json:"unique"`
	Primary    bool   `json:"primary"`
	Definition string `json:"definition"`
}

type apiTableMeta struct {
	Schema      string          `json:"schema"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	KindLabel   string          `json:"kind_label"`
	Columns     []apiColumn     `json:"columns"`
	Constraints []apiConstraint `json:"constraints"`
	Indexes     []apiIndex      `json:"indexes"`
}

type apiSchema struct {
	Name   string         `json:"name"`
	Tables []apiTableMeta `json:"tables"`
}

type dbDetailResponse struct {
	ID      int64       `json:"id"`
	Name    string      `json:"name"`
	Engine  string      `json:"engine"`
	Level   string      `json:"level"`
	CanEdit bool        `json:"can_edit"`
	Schemas []apiSchema `json:"schemas"`
}

type apiCell struct {
	Text      string `json:"text"`
	Null      bool   `json:"null"`
	Truncated bool   `json:"truncated"`
}

type apiPagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	From       int64 `json:"from"`
	To         int64 `json:"to"`
	HasPrev    bool  `json:"has_prev"`
	HasNext    bool  `json:"has_next"`
}

type tablePageResponse struct {
	DatabaseID int64         `json:"database_id"`
	Schema     string        `json:"schema"`
	Name       string        `json:"name"`
	Kind       string        `json:"kind"`
	Columns    []string      `json:"columns"`
	Rows       [][]apiCell   `json:"rows"`
	Pagination apiPagination `json:"pagination"`
	Order      string        `json:"order"`
}

func (e *testEnv) apiMember(username string, connID int64, level authz.Level) *apiClient {
	e.t.Helper()
	gid := testutil.SeedGroup(e.t, e.db, "grup-"+username)
	c := e.apiUser(username, false)
	testutil.SeedMember(e.t, e.db, gid, e.userID(username))
	testutil.SeedGrant(e.t, e.db, gid, connID, level)
	return c
}

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

func findTable(t *testing.T, d dbDetailResponse, schema, name string) apiTableMeta {
	t.Helper()
	for _, s := range d.Schemas {
		if s.Name != schema {
			continue
		}
		for _, tbl := range s.Tables {
			if tbl.Name == name {
				return tbl
			}
		}
	}
	t.Fatalf("tabel %s.%s tidak ada di metadata", schema, name)
	return apiTableMeta{}
}

func firstCellContains(t *testing.T, page tablePageResponse, needle string) bool {
	t.Helper()
	for _, row := range page.Rows {
		for _, cell := range row {
			if strings.Contains(cell.Text, needle) {
				return true
			}
		}
	}
	return false
}

func TestDatabaseListShowsOnlyAccessibleWithEffectiveLevel(t *testing.T) {
	e := setup(t)
	a := testutil.SeedConnection(t, e.db)
	b := testutil.SeedConnection(t, e.db)
	c := testutil.SeedConnection(t, e.db)
	alice := e.apiUser("alice", false)
	viewers := testutil.SeedGroup(t, e.db, "viewers")
	editors := testutil.SeedGroup(t, e.db, "editors")
	testutil.SeedMember(t, e.db, viewers, e.userID("alice"))
	testutil.SeedMember(t, e.db, editors, e.userID("alice"))
	testutil.SeedGrant(t, e.db, viewers, a.ID, authz.LevelView)
	testutil.SeedGrant(t, e.db, viewers, b.ID, authz.LevelView)
	testutil.SeedGrant(t, e.db, editors, b.ID, authz.LevelEdit)

	w := alice.json("GET", "/api/databases", nil)
	expectStatus(t, w, http.StatusOK)
	var list dbListResponse
	decodeBody(t, w, &list)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("daftar database: %+v", list)
	}
	levels := map[int64]string{}
	for _, item := range list.Items {
		levels[item.ID] = item.Level
		if item.Name == c.Name {
			t.Fatal("database tanpa grant ikut tampil")
		}
	}
	if levels[a.ID] != "view" || levels[b.ID] != "edit" {
		t.Fatalf("level efektif salah: %+v", levels)
	}
	if strings.Contains(w.Body.String(), "127.0.0.1") || strings.Contains(w.Body.String(), "5435") {
		t.Fatal("detail host atau port bocor ke user")
	}
}

func TestDatabaseListEmptyStates(t *testing.T) {
	e := setup(t)
	testutil.SeedConnection(t, e.db)
	alice := e.apiUser("alice", false)
	admin := e.apiUser("admin", true)
	for name, c := range map[string]*apiClient{"user tanpa grup": alice, "admin tanpa grant": admin} {
		w := c.json("GET", "/api/databases", nil)
		expectStatus(t, w, http.StatusOK)
		var list dbListResponse
		decodeBody(t, w, &list)
		if list.Total != 0 || len(list.Items) != 0 {
			t.Fatalf("%s: daftar harus kosong, dapat %+v", name, list)
		}
	}
	expectAPIError(t, e.api().json("GET", "/api/databases", nil), http.StatusUnauthorized, "unauthenticated")
}

func TestDatabaseViewerSchemaAndData(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	alice := e.apiMember("alice", conn.ID, authz.LevelView)
	base := fmt.Sprintf("/api/databases/%d", conn.ID)

	w := alice.json("GET", base, nil)
	expectStatus(t, w, http.StatusOK)
	var detail dbDetailResponse
	decodeBody(t, w, &detail)
	if detail.ID != conn.ID || detail.Name != conn.Name || detail.Engine != dbconn.EnginePostgres || detail.Level != "view" || detail.CanEdit {
		t.Fatalf("metadata database: %+v", detail)
	}
	schemas := map[string]bool{}
	for _, s := range detail.Schemas {
		schemas[s.Name] = true
	}
	for _, want := range []string{"public", "analytics"} {
		if !schemas[want] {
			t.Fatalf("schema %s tidak ada", want)
		}
	}
	customers := findTable(t, detail, "public", "customers")
	if customers.Kind != "table" || customers.KindLabel != "Table" || len(customers.Columns) == 0 || len(customers.Constraints) == 0 || len(customers.Indexes) == 0 {
		t.Fatalf("metadata customers tidak lengkap: %+v", customers)
	}
	var hasTimestampType, hasPK, hasUnique, hasIndex bool
	for _, col := range customers.Columns {
		if col.Name == "created_at" && col.Type == "timestamp with time zone" {
			hasTimestampType = true
		}
	}
	for _, con := range customers.Constraints {
		if con.Name == "customers_pkey" && con.Type == "PRIMARY KEY" {
			hasPK = true
		}
		if con.Name == "customers_email_key" && con.Type == "UNIQUE" {
			hasUnique = true
		}
	}
	for _, idx := range customers.Indexes {
		if idx.Name == "customers_city_idx" {
			hasIndex = true
		}
	}
	if !hasTimestampType || !hasPK || !hasUnique || !hasIndex {
		t.Fatalf("metadata customers tidak benar: %+v", customers)
	}
	findTable(t, detail, "public", "Legacy Reports")
	findTable(t, detail, "analytics", "events")
	orderTotals := findTable(t, detail, "public", "order_totals")
	if orderTotals.Kind != "view" || orderTotals.KindLabel != "View" {
		t.Fatalf("metadata view: %+v", orderTotals)
	}

	tablePath := base + "/tables/public/customers"
	w = alice.json("GET", tablePath, nil)
	expectStatus(t, w, http.StatusOK)
	var page tablePageResponse
	decodeBody(t, w, &page)
	if page.DatabaseID != conn.ID || page.Schema != "public" || page.Name != "customers" || page.Order != "primary key" {
		t.Fatalf("halaman tabel: %+v", page)
	}
	if page.Pagination.Total != 200 || page.Pagination.PageSize != 20 || page.Pagination.From != 1 || page.Pagination.To != 20 || !page.Pagination.HasNext || page.Pagination.HasPrev {
		t.Fatalf("paginasi: %+v", page.Pagination)
	}
	if !firstCellContains(t, page, "Customer 001") || firstCellContains(t, page, "Customer 021") {
		t.Fatal("isi halaman pertama tidak sesuai")
	}
	for _, col := range []string{"id", "name", "email"} {
		found := false
		for _, got := range page.Columns {
			if got == col {
				found = true
			}
		}
		if !found {
			t.Fatalf("kolom %s tidak ada", col)
		}
	}

	cases := []struct {
		query string
		from  int64
		to    int64
		page  int
	}{
		{"?page=10", 181, 200, 10},
		{"?page=99", 181, 200, 10},
		{"?page=2&page_size=10", 11, 20, 2},
	}
	for _, tc := range cases {
		w := alice.json("GET", tablePath+tc.query, nil)
		expectStatus(t, w, http.StatusOK)
		decodeBody(t, w, &page)
		if page.Pagination.From != tc.from || page.Pagination.To != tc.to || page.Pagination.Page != tc.page {
			t.Fatalf("%s: paginasi %+v", tc.query, page.Pagination)
		}
	}
	for _, q := range []string{"?page=0", "?page=-3", "?page=abc", "?page_size=0", "?page_size=1000"} {
		expectAPIError(t, alice.json("GET", tablePath+q, nil), http.StatusUnprocessableEntity, "validation_error")
	}

	w = alice.json("GET", base+"/tables/public/legacy_notes", nil)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &page)
	if page.Order != "ctid" {
		t.Fatalf("urutan legacy_notes: %s", page.Order)
	}
	var nullCell, truncatedCell, htmlCell bool
	for _, row := range page.Rows {
		for _, cell := range row {
			if cell.Null {
				nullCell = true
			}
			if cell.Truncated && len(cell.Text) > 400 {
				truncatedCell = true
			}
			if strings.Contains(cell.Text, "<b>tag HTML</b>") {
				htmlCell = true
			}
		}
	}
	if !nullCell || !truncatedCell || !htmlCell {
		t.Fatalf("sel NULL, terpotong, atau HTML tidak benar: %+v", page.Rows)
	}

	w = alice.json("GET", base+"/tables/public/order_totals", nil)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &page)
	if page.Order != "none" || page.Kind != "view" {
		t.Fatalf("urutan view tanpa PK: %+v", page)
	}

	w = alice.json("GET", base+"/tables/public/Legacy%20Reports", nil)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &page)
	if !firstCellContains(t, page, "Laporan Q1") {
		t.Fatal("nama tabel dengan spasi tidak bisa dibaca")
	}
	w = alice.json("GET", base+"/tables/analytics/events", nil)
	expectStatus(t, w, http.StatusOK)
	decodeBody(t, w, &page)
	if page.Pagination.Total != 250 {
		t.Fatalf("jumlah baris events: %+v", page.Pagination)
	}

	for _, p := range []string{"/tables/public/tidak_ada", "/tables/pg_catalog/pg_class", "/tables/public/pg_class", "/tables/public/customers%3Bdrop", "/tables/nope/customers", "/tables/public/customers%22"} {
		expectAPIError(t, alice.json("GET", base+p, nil), http.StatusNotFound, "not_found")
	}
	expectAPIError(t, alice.json("GET", "/api/databases/abc", nil), http.StatusNotFound, "not_found")
	expectAPIError(t, alice.json("GET", "/api/databases/0/tables/public/customers", nil), http.StatusNotFound, "not_found")
}

func TestDatabaseViewerAccessControl(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	other := testutil.SeedConnection(t, e.db)
	viewer := e.apiMember("viewer", conn.ID, authz.LevelView)
	editor := e.apiMember("editor", conn.ID, authz.LevelEdit)
	noGrant := e.apiUser("tanpagrant", false)
	outsider := e.apiMember("luar", other.ID, authz.LevelEdit)
	admin := e.apiUser("admin", true)
	emptyGroup := testutil.SeedGroup(t, e.db, "kosong")
	temp := e.apiUser("anggota", false)
	testutil.SeedMember(t, e.db, emptyGroup, e.userID("anggota"))
	testutil.SeedGrant(t, e.db, emptyGroup, other.ID, authz.LevelView)

	paths := []string{fmt.Sprintf("/api/databases/%d", conn.ID), fmt.Sprintf("/api/databases/%d/tables/public/customers", conn.ID)}
	for _, p := range paths {
		expectStatus(t, viewer.json("GET", p, nil), http.StatusOK)
		expectStatus(t, editor.json("GET", p, nil), http.StatusOK)
		for _, c := range []*apiClient{noGrant, outsider, temp, admin} {
			expectAPIError(t, c.json("GET", p, nil), http.StatusForbidden, "forbidden")
		}
		expectAPIError(t, e.api().json("GET", p, nil), http.StatusUnauthorized, "unauthenticated")
	}
	for _, c := range []*apiClient{noGrant, admin} {
		expectAPIError(t, c.json("GET", "/api/databases/99999", nil), http.StatusForbidden, "forbidden")
		expectAPIError(t, c.json("GET", "/api/databases/99999/tables/public/customers", nil), http.StatusForbidden, "forbidden")
	}
	expectAPIError(t, viewer.json("GET", "/api/databases/99999", nil), http.StatusForbidden, "forbidden")

	gid := testutil.SeedGroup(t, e.db, "sementara")
	user := e.apiUser("sementara", false)
	testutil.SeedMember(t, e.db, gid, e.userID("sementara"))
	testutil.SeedGrant(t, e.db, gid, conn.ID, authz.LevelView)
	expectStatus(t, user.json("GET", paths[0], nil), http.StatusOK)
	if err := e.app.Grants.RemoveAccess(context.Background(), gid, conn.ID); err != nil {
		t.Fatal(err)
	}
	expectAPIError(t, user.json("GET", paths[0], nil), http.StatusUnauthorized, "session_expired")
}

func TestDatabaseConnectionErrorJSON(t *testing.T) {
	e := setup(t)
	dead := testutil.SeedConnection(t, e.db, func(in *dbconn.Input) { in.Port = closedPort; in.Password = secretPW })
	live := testutil.SeedConnection(t, e.db)
	alice := e.apiMember("alice", dead.ID, authz.LevelEdit)
	liveGroup := testutil.SeedGroup(t, e.db, "hidup")
	testutil.SeedMember(t, e.db, liveGroup, e.userID("alice"))
	testutil.SeedGrant(t, e.db, liveGroup, live.ID, authz.LevelView)
	admin := e.apiUser("admin", true)
	adminGroup := testutil.SeedGroup(t, e.db, "grup-admin")
	testutil.SeedMember(t, e.db, adminGroup, e.userID("admin"))
	testutil.SeedGrant(t, e.db, adminGroup, dead.ID, authz.LevelView)

	base := fmt.Sprintf("/api/databases/%d", dead.ID)
	for _, p := range []string{base, base + "/tables/public/customers"} {
		w := alice.json("GET", p, nil)
		expectAPIError(t, w, http.StatusServiceUnavailable, "target_unavailable")
		body := w.Body.String()
		contains(t, body, dbconn.KindRefused.UserMessage())
		notContains(t, body, "127.0.0.1", fmt.Sprint(closedPort), secretPW, "dial tcp", "connect:")
		w = admin.json("GET", p, nil)
		expectAPIError(t, w, http.StatusServiceUnavailable, "target_unavailable")
		notContains(t, w.Body.String(), "127.0.0.1", secretPW)
	}
	expectStatus(t, alice.json("GET", "/api/me", nil), http.StatusOK)
	expectStatus(t, alice.json("GET", "/api/databases", nil), http.StatusOK)
	expectStatus(t, alice.json("GET", fmt.Sprintf("/api/databases/%d", live.ID), nil), http.StatusOK)
	logs := e.log.String()
	contains(t, logs, "target database unreachable")
	notContains(t, logs, secretPW, "127.0.0.1:5499")
}

func TestConnectionUpdateInvalidatesViewerPool(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db)
	alice := e.apiMember("alice", conn.ID, authz.LevelView)
	admin := e.apiUser("admin", true)
	page := fmt.Sprintf("/api/databases/%d", conn.ID)
	update := func(port int) {
		t.Helper()
		body := connInput(conn.Name)
		body.Password = ""
		body.Port = port
		expectStatus(t, admin.json("PATCH", fmt.Sprintf("/api/admin/connections/%d", conn.ID), body), http.StatusOK)
	}
	expectStatus(t, alice.json("GET", page, nil), http.StatusOK)
	update(closedPort)
	expectAPIError(t, alice.json("GET", page, nil), http.StatusServiceUnavailable, "target_unavailable")
	update(5435)
	expectStatus(t, alice.json("GET", page, nil), http.StatusOK)
	e.noSecretInLogs()
}
