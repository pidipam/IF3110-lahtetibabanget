package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/schemadiff"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

const schemaDiffBooksDDL = `
CREATE TABLE zz_sd_app_books (
    id serial NOT NULL,
    title text NOT NULL,
    CONSTRAINT zz_sd_app_books_pkey PRIMARY KEY (id)
);
CREATE INDEX zz_sd_app_books_title_idx ON zz_sd_app_books (title);
CREATE VIEW zz_sd_app_books_v AS
    SELECT id, title FROM zz_sd_app_books;
`

type schemaDiffFixture struct {
	e *testEnv
	pool *pgxpool.Pool
	conn *dbconn.Connection
	base string
	dump string
	ed *apiClient
	view *apiClient
	guest *apiClient
	admin *apiClient
}

type schemaDiffSource struct {
	DatabaseName string `json:"database_name"`
	Schemas []string `json:"schemas"`
	Schema string `json:"schema"`
	CurrentDDL string `json:"current_ddl"`
}

type schemaDiffResult struct {
	Changes []struct {
		Op string `json:"op"`
		OpLabel string `json:"op_label"`
		Object string `json:"object"`
		Destructive bool `json:"destructive"`
	} `json:"changes"`
	Warnings []string `json:"warnings"`
	SQL string `json:"sql"`
	Statements int `json:"statements"`
	Skipped int `json:"skipped"`
	Title string `json:"title"`
	CanCreate bool `json:"can_create"`
}

func setupSchemaDiff(t *testing.T) *schemaDiffFixture {
	t.Helper()
	e := setup(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testutil.RequireEnv(t, testutil.EnvTargetDB))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cleanupSchemaDiffObjects(t, pool)
	t.Cleanup(func() { cleanupSchemaDiffObjects(t, pool) })
	conn := testutil.SeedConnection(t, e.db)
	f := &schemaDiffFixture{e: e, pool: pool, conn: conn, base: fmt.Sprintf("/api/databases/%d/schema-diff", conn.ID), ed: e.apiMember("sd_editor", conn.ID, authz.LevelEdit), view: e.apiMember("sd_viewer", conn.ID, authz.LevelView), guest: e.apiUser("sd_guest", false), admin: e.apiUser("sd_admin", true)}
	cur, err := schemadiff.Load(ctx, pool, "public")
	if err != nil {
		t.Fatal(err)
	}
	f.dump = schemadiff.Dump(cur)
	if strings.TrimSpace(f.dump) == "" {
		t.Fatal("dump schema public kosong")
	}
	return f
}

func (f *schemaDiffFixture) preview(c *apiClient, ddl string, destructive bool) *httptest.ResponseRecorder {
	return c.json("POST", f.base, map[string]any{"schema": "public", "desired_ddl": ddl, "include_destructive": destructive})
}

func (f *schemaDiffFixture) result(t *testing.T, ddl string, destructive bool) schemaDiffResult {
	t.Helper()
	w := f.preview(f.ed, ddl, destructive)
	expectStatus(t, w, http.StatusOK)
	var out schemaDiffResult
	decodeBody(t, w, &out)
	return out
}

func cleanupSchemaDiffObjects(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT c.relname, c.relkind::text FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname LIKE 'zz\_sd\_app\_%' AND c.relkind IN ('r', 'v')
		ORDER BY c.relkind = 'v' DESC`)
	if err != nil {
		t.Fatal(err)
	}
	type rel struct{ name, kind string }
	var rels []rel
	for rows.Next() {
		var r rel
		if err := rows.Scan(&r.name, &r.kind); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rels = append(rels, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, r := range rels {
		kind := "TABLE"
		if r.kind == "v" {
			kind = "VIEW"
		}
		if _, err := pool.Exec(ctx, "DROP "+kind+" IF EXISTS public."+pgx.Identifier{r.name}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
}

func schemaDiffScratchCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'zz\_schemadiff\_%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func schemaDiffRelationExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestSchemaDiffAccessControl(t *testing.T) {
	f := setupSchemaDiff(t)
	e := f.e
	expectAPIError(t, e.api().json("GET", f.base, nil), http.StatusUnauthorized, "unauthenticated")
	for name, c := range map[string]*apiClient{"view only": f.view, "tanpa grant": f.guest, "admin tanpa grant": f.admin} {
		w := c.json("GET", f.base, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: GET status %d, want 403", name, w.Code)
		}
		notContains(t, w.Body.String(), "CREATE TABLE")
		expectStatus(t, f.preview(c, "SELECT 1;", false), http.StatusForbidden)
	}
	w := f.ed.json("GET", f.base, nil)
	expectStatus(t, w, http.StatusOK)
	var src schemaDiffSource
	decodeBody(t, w, &src)
	if src.Schema != "public" || src.DatabaseName != f.conn.Name || !strings.Contains(src.CurrentDDL, "CREATE TABLE") || len(src.Schemas) == 0 {
		t.Fatalf("sumber schema diff salah: %+v", src)
	}
	expectStatus(t, f.ed.json("GET", "/api/databases/abc/schema-diff", nil), http.StatusNotFound)
	expectStatus(t, f.ed.json("GET", "/api/databases/9999/schema-diff", nil), http.StatusForbidden)
	expectInvalidField(t, f.ed.json("GET", f.base+"?schema=tidak_ada", nil), "schema")
	if _, err := e.db.Exec(context.Background(), "UPDATE group_db_access SET level = 'view' WHERE db_id = $1", f.conn.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.ed.json("GET", f.base, nil), http.StatusForbidden)
}

func TestSchemaDiffPreview(t *testing.T) {
	f := setupSchemaDiff(t)
	before := schemaDiffScratchCount(t, f.pool)
	if res := f.result(t, f.dump, false); len(res.Changes) != 0 || res.CanCreate {
		t.Fatalf("schema yang sama harus tanpa perbedaan: %+v", res)
	}
	res := f.result(t, f.dump+"\n"+schemaDiffBooksDDL, false)
	if !res.CanCreate || res.Statements == 0 || !strings.HasPrefix(res.Title, "Schema diff public") {
		t.Fatalf("hasil tambah tabel salah: %+v", res)
	}
	contains(t, res.SQL, "CREATE TABLE public.zz_sd_app_books", "CREATE VIEW public.zz_sd_app_books_v")
	notContains(t, res.SQL, "zz_schemadiff_")
	added := false
	for _, c := range res.Changes {
		added = added || (c.Op == "add" && c.OpLabel == "Tambah" && strings.Contains(c.Object, "zz_sd_app_books"))
	}
	if !added {
		t.Fatalf("perubahan tambah tabel tidak tercatat: %+v", res.Changes)
	}
	if schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books") {
		t.Fatal("preview mengubah database target")
	}
	res = f.result(t, schemaDiffBooksDDL, false)
	if res.Skipped == 0 {
		t.Fatalf("perubahan destruktif harus dilewati: %+v", res)
	}
	contains(t, res.SQL, "-- DROP TABLE public.customers;")
	res = f.result(t, schemaDiffBooksDDL, true)
	contains(t, res.SQL, "DROP TABLE public.customers;", "PERINGATAN: menghapus data.")
	notContains(t, res.SQL, "-- DROP TABLE public.customers;")
	if got := schemaDiffScratchCount(t, f.pool); got != before {
		t.Fatalf("schema sementara tertinggal: sebelum %d, sesudah %d", before, got)
	}
	if schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books") {
		t.Fatal("preview mengubah database target")
	}
}

func TestSchemaDiffPreviewRejectsInvalidInput(t *testing.T) {
	f := setupSchemaDiff(t)
	before := schemaDiffScratchCount(t, f.pool)
	cases := []struct {
		name, ddl, want string
	}{
		{"DDL salah", "CREATE TABLE zz_sd_broken (id tipe_tidak_ada);", "DDL tidak valid"},
		{"DDL tidak lengkap", "CREATE TABLE zz_sd_broken (", "DDL tidak valid"},
		{"kontrol transaksi", "COMMIT;", "BEGIN, COMMIT"},
		{"DDL kosong", "   \n\t", "SQL wajib diisi"},
		{"karakter NUL", "CREATE TABLE t (id int);\x00", "karakter tidak valid"},
	}
	for _, tc := range cases {
		w := f.preview(f.ed, tc.ddl, false)
		expectInvalidField(t, w, "desired_ddl")
		contains(t, w.Body.String(), tc.want)
		notContains(t, w.Body.String(), "postgres://", "sslmode", "dial tcp")
	}
	expectInvalidField(t, f.ed.json("POST", f.base, map[string]any{"schema": " ", "desired_ddl": f.dump}), "schema")
	if got := schemaDiffScratchCount(t, f.pool); got != before {
		t.Fatalf("schema sementara tertinggal setelah input salah: %d", got)
	}
	if schemaDiffRelationExists(t, f.pool, "public.zz_sd_broken") {
		t.Fatal("DDL gagal mengubah database target")
	}
	expectStatus(t, f.ed.send("POST", f.base, `{"schema":"public","desired_ddl":"SELECT 1;"}`, ""), http.StatusForbidden)
}

func TestSchemaDiffSaveAsMigrationAndExecute(t *testing.T) {
	f := setupSchemaDiff(t)
	res := f.result(t, f.dump+"\n"+schemaDiffBooksDDL, false)
	savedSQL := res.SQL + "\n-- disesuaikan oleh pengguna\n"
	body := map[string]any{"title": "schema diff buku", "sql_up": savedSQL, "sql_down": "DROP VIEW public.zz_sd_app_books_v; DROP TABLE public.zz_sd_app_books;", "is_irreversible": false, "is_batch_mode": false}
	w := f.ed.json("POST", fmt.Sprintf("/api/databases/%d/migrations", f.conn.ID), body)
	expectStatus(t, w, http.StatusCreated)
	var m spaMigration
	decodeBody(t, w, &m)
	if f.e.count("SELECT count(*) FROM platform_migration_revisions WHERE migration_id = $1 AND sql_up LIKE '%disesuaikan oleh pengguna%'", m.ID) != 1 {
		t.Fatal("SQL yang disimpan bukan SQL yang diedit pengguna")
	}
	leader := f.e.createUser("sd_leader", "password123", false)
	var gEditor, gViewer int64
	if err := f.e.db.QueryRow(context.Background(), "SELECT max(group_id) FILTER (WHERE level = 'edit'), max(group_id) FILTER (WHERE level = 'view') FROM group_db_access WHERE db_id = $1", f.conn.ID).Scan(&gEditor, &gViewer); err != nil {
		t.Fatal(err)
	}
	testutil.SeedMember(t, f.e.db, gEditor, leader.ID)
	f.e.setLeader(gEditor, leader.ID)
	f.e.setLeader(gViewer, f.e.userID("sd_viewer"))
	expectStatus(t, f.ed.json("POST", "/api/migrations/"+m.ID+"/submit", nil), http.StatusOK)
	for _, c := range []*apiClient{f.e.apiLogin("sd_leader", "password123"), f.view} {
		expectStatus(t, c.json("POST", "/api/migrations/"+m.ID+"/approve", map[string]any{"revision_number": 1}), http.StatusOK)
	}
	w = f.ed.json("POST", "/api/migrations/"+m.ID+"/execute", nil)
	expectStatus(t, w, http.StatusAccepted)
	var ex struct {
		JobID string `json:"job_id"`
	}
	decodeBody(t, w, &ex)
	if j := waitJob(t, f.ed, ex.JobID, "success", "failed"); j.Status != "success" {
		t.Fatalf("job schema diff: %+v", j)
	}
	if !schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books") || !schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books_v") {
		t.Fatal("tabel atau view hasil migration tidak terbentuk")
	}
	res = f.result(t, f.dump+"\n"+schemaDiffBooksDDL, false)
	if strings.Contains(res.SQL, "CREATE TABLE public.zz_sd_app_books") {
		t.Fatal("schema diff masih mengusulkan tabel yang sudah ada")
	}
	logs := f.e.log.String()
	notContains(t, logs, "zz_sd_app_books", "CREATE TABLE", "desired_ddl", "password", "postgres://")
	notContains(t, logs, f.ed.session())
}

func TestSchemaDiffTargetDown(t *testing.T) {
	e := setup(t)
	dead := testutil.SeedConnection(t, e.db, func(in *dbconn.Input) { in.Port = closedPort; in.Password = secretPW })
	ed := e.apiMember("sd_dead_editor", dead.ID, authz.LevelEdit)
	base := fmt.Sprintf("/api/databases/%d/schema-diff", dead.ID)
	w := ed.json("GET", base, nil)
	expectAPIError(t, w, http.StatusServiceUnavailable, "target_unavailable")
	contains(t, w.Body.String(), dbconn.KindRefused.UserMessage())
	notContains(t, w.Body.String(), "127.0.0.1", fmt.Sprint(closedPort), secretPW, "dial tcp", "connect:")
	w = ed.json("POST", base, map[string]any{"schema": "public", "desired_ddl": "SELECT 1;"})
	expectAPIError(t, w, http.StatusServiceUnavailable, "target_unavailable")
	notContains(t, w.Body.String(), "127.0.0.1", secretPW)
	e.noSecretInLogs()
}

func TestSchemaDiffRejectsMySQL(t *testing.T) {
	e := setup(t)
	conn := testutil.SeedConnection(t, e.db, func(in *dbconn.Input) { in.Engine = dbconn.EngineMariaDB; in.Port = closedPort })
	ed := e.apiMember("sd_maria", conn.ID, authz.LevelEdit)
	expectAPIError(t, ed.json("GET", fmt.Sprintf("/api/databases/%d/schema-diff", conn.ID), nil), http.StatusUnprocessableEntity, "schema_diff_unsupported")
}
