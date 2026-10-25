package app

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
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
	e     *testEnv
	pool  *pgxpool.Pool
	conn  *dbconn.Connection
	base  string
	dump  string
	ed    *client
	view  *client
	guest *client
	admin *client
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
	f := &schemaDiffFixture{
		e: e, pool: pool, conn: conn,
		base:  fmt.Sprintf("/databases/%d/schema-diff", conn.ID),
		ed:    e.member("sd_editor", conn.ID, authz.LevelEdit),
		view:  e.member("sd_viewer", conn.ID, authz.LevelView),
		guest: e.loginAs("sd_guest", false),
		admin: e.loginAs("sd_admin", true),
	}
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

var textareaPattern = regexp.MustCompile(`(?s)<textarea id="([^"]+)"[^>]*>(.*?)</textarea>`)

func textareaValue(t *testing.T, body, id string) string {
	t.Helper()
	for _, m := range textareaPattern.FindAllStringSubmatch(body, -1) {
		if m[1] == id {
			return html.UnescapeString(strings.TrimSpace(m[2]))
		}
	}
	t.Fatalf("textarea %q tidak ditemukan", id)
	return ""
}

func TestSchemaDiffAccessControl(t *testing.T) {
	f := setupSchemaDiff(t)
	e := f.e

	anon := e.client().do("GET", f.base, nil)
	expectStatus(t, anon, http.StatusSeeOther)
	if anon.Header().Get("Location") != "/login" {
		t.Fatalf("anonim diarahkan ke %q", anon.Header().Get("Location"))
	}

	for name, c := range map[string]*client{"view only": f.view, "tanpa grant": f.guest, "admin tanpa grant": f.admin} {
		w := c.do("GET", f.base, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: GET status %d, want 403", name, w.Code)
		}
		notContains(t, w.Body.String(), "desired_ddl", "CREATE TABLE")
		expectStatus(t, c.post(f.base, url.Values{"schema": {"public"}, "desired_ddl": {"SELECT 1;"}}), http.StatusForbidden)
		expectStatus(t, c.post(f.base+"/migration", url.Values{"title": {"x"}, "schema": {"public"}, "sql": {"SELECT 1;"}}), http.StatusForbidden)
	}

	detail := f.view.do("GET", fmt.Sprintf("/databases/%d", f.conn.ID), nil)
	expectStatus(t, detail, http.StatusOK)
	notContains(t, detail.Body.String(), "Schema diff")
	notContains(t, f.guest.do("GET", fmt.Sprintf("/databases/%d", f.conn.ID), nil).Body.String(), "Schema diff")

	w := f.ed.do("GET", f.base, nil)
	expectStatus(t, w, http.StatusOK)
	contains(t, w.Body.String(), "Schema diff", `name="desired_ddl"`, "DDL kondisi yang diinginkan", "Lihat DDL kondisi saat ini", "CREATE TABLE")
	contains(t, f.ed.do("GET", fmt.Sprintf("/databases/%d", f.conn.ID), nil).Body.String(), "Schema diff")

	expectStatus(t, f.ed.do("GET", "/databases/abc/schema-diff", nil), http.StatusNotFound)
	expectStatus(t, f.ed.do("GET", "/databases/9999/schema-diff", nil), http.StatusForbidden)
	expectStatus(t, f.ed.do("GET", f.base+"?schema=tidak_ada", nil), http.StatusUnprocessableEntity)

	if _, err := f.e.db.Exec(context.Background(), "UPDATE group_db_access SET level = 'view' WHERE group_id = $1", e.userGroup("sd_editor")); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.ed.do("GET", f.base, nil), http.StatusForbidden)
	notContains(t, f.ed.do("GET", fmt.Sprintf("/databases/%d", f.conn.ID), nil).Body.String(), "Schema diff")
}

func TestSchemaDiffPreview(t *testing.T) {
	f := setupSchemaDiff(t)
	before := schemaDiffScratchCount(t, f.pool)

	preview := func(form url.Values) *httptest.ResponseRecorder {
		return f.ed.post(f.base, form)
	}

	w := preview(url.Values{"schema": {"public"}, "desired_ddl": {f.dump}})
	expectStatus(t, w, http.StatusOK)
	contains(t, w.Body.String(), "Tidak ada perbedaan")

	desired := f.dump + "\n" + schemaDiffBooksDDL
	w = preview(url.Values{"schema": {"public"}, "desired_ddl": {desired}})
	expectStatus(t, w, http.StatusOK)
	body := w.Body.String()
	contains(t, body, "zz_sd_app_books", "Tambah", "CREATE TABLE public.zz_sd_app_books",
		`name="title"`, `name="sql"`, "Simpan sebagai draft migration")
	notContains(t, body, "zz_schemadiff_")
	if schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books") {
		t.Fatal("preview mengubah database target")
	}

	w = preview(url.Values{"schema": {"public"}, "desired_ddl": {schemaDiffBooksDDL}})
	expectStatus(t, w, http.StatusOK)
	body = w.Body.String()
	contains(t, body, "DILEWATI", "destruktif", "statement aktif", "Hapus")
	contains(t, body, "-- DROP TABLE public.customers;")

	w = preview(url.Values{"schema": {"public"}, "desired_ddl": {schemaDiffBooksDDL}, "include_destructive": {"1"}})
	expectStatus(t, w, http.StatusOK)
	body = w.Body.String()
	contains(t, body, "DROP TABLE public.customers;", "PERINGATAN: menghapus data.")
	notContains(t, body, "-- DROP TABLE public.customers;")

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
	if n := f.e.count("SELECT count(*) FROM migrations"); n != 0 {
		t.Fatalf("fixture tidak bersih: %d migration", n)
	}

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
		w := f.ed.post(f.base, url.Values{"schema": {"public"}, "desired_ddl": {tc.ddl}})
		expectStatus(t, w, http.StatusUnprocessableEntity)
		contains(t, w.Body.String(), tc.want)
		notContains(t, w.Body.String(), "postgres://", "sslmode", "dial tcp")
	}
	if got := schemaDiffScratchCount(t, f.pool); got != before {
		t.Fatalf("schema sementara tertinggal setelah input salah: %d", got)
	}
	if schemaDiffRelationExists(t, f.pool, "public.zz_sd_broken") {
		t.Fatal("DDL gagal mengubah database target")
	}

	form := url.Values{"schema": {"public"}, "desired_ddl": {f.dump}}
	expectStatus(t, f.ed.do("POST", f.base, form), http.StatusForbidden)
	expectStatus(t, f.ed.do("POST", f.base+"/migration", url.Values{"title": {"x"}, "schema": {"public"}, "sql": {"SELECT 1;"}}), http.StatusForbidden)
	if n := f.e.count("SELECT count(*) FROM migrations"); n != 0 {
		t.Fatal("draft tersimpan tanpa CSRF")
	}
}

func TestSchemaDiffSaveDraftAndExecute(t *testing.T) {
	f := setupSchemaDiff(t)

	desired := f.dump + "\n" + schemaDiffBooksDDL
	w := f.ed.post(f.base, url.Values{"schema": {"public"}, "desired_ddl": {desired}})
	expectStatus(t, w, http.StatusOK)
	generated := textareaValue(t, w.Body.String(), "sql")
	contains(t, generated, "CREATE TABLE public.zz_sd_app_books", "CREATE VIEW public.zz_sd_app_books_v")
	notContains(t, generated, "zz_schemadiff_")

	savedSQL := generated + "\n-- disesuaikan oleh pengguna\n"
	form := url.Values{"title": {"schema diff buku"}, "schema": {"public"}, "sql": {savedSQL}}
	create := f.ed.post(f.base+"/migration", form)
	expectStatus(t, create, http.StatusSeeOther)
	loc := create.Header().Get("Location")
	if !strings.HasPrefix(loc, migBase(f.conn.ID)+"/") || !strings.Contains(loc, "ok=schema_diff_saved") {
		t.Fatalf("redirect draft salah: %s", loc)
	}
	path := strings.SplitN(loc, "?", 2)[0]

	if f.e.count("SELECT count(*) FROM migrations WHERE db_id = $1 AND status = 'draft' AND version IS NULL", f.conn.ID) != 1 {
		t.Fatal("draft migration tidak tersimpan")
	}
	if f.e.count("SELECT count(*) FROM migrations WHERE db_id = $1 AND sql_text LIKE '%disesuaikan oleh pengguna%'", f.conn.ID) != 1 {
		t.Fatal("SQL yang disimpan bukan SQL yang diedit pengguna")
	}

	detail := f.ed.do("GET", loc, nil)
	expectStatus(t, detail, http.StatusOK)
	contains(t, detail.Body.String(), "berhasil dibuat", "schema diff buku")

	review := f.ed.do("GET", path+"/review", nil)
	expectStatus(t, review, http.StatusOK)
	contains(t, review.Body.String(), "CREATE TABLE public.zz_sd_app_books")

	sum := migration.SQLChecksum(strings.TrimSpace(savedSQL))
	expectStatus(t, f.ed.post(path+"/execute", url.Values{"checksum": {"salah"}, "confirm": {"on"}}), http.StatusConflict)
	if schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books") {
		t.Fatal("eksekusi dengan checksum salah mengubah target")
	}
	exec := f.ed.post(path+"/execute", url.Values{"checksum": {sum}, "confirm": {"on"}})
	expectStatus(t, exec, http.StatusSeeOther)
	if !strings.Contains(exec.Header().Get("Location"), "ok=migration_success") {
		t.Fatalf("redirect eksekusi salah: %s", exec.Header().Get("Location"))
	}
	if !schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books") || !schemaDiffRelationExists(t, f.pool, "public.zz_sd_app_books_v") {
		t.Fatal("tabel atau view hasil migration tidak terbentuk")
	}
	if f.e.count("SELECT count(*) FROM migrations WHERE db_id = $1 AND status = 'success' AND version = 1", f.conn.ID) != 1 {
		t.Fatal("migration sukses tidak tercatat dengan versi 1")
	}

	w = f.view.post(f.base+"/migration", url.Values{"title": {"retas"}, "schema": {"public"}, "sql": {"SELECT 1;"}})
	expectStatus(t, w, http.StatusForbidden)

	w = f.ed.post(f.base+"/migration", url.Values{"title": {"  "}, "schema": {"public"}, "sql": {"SELECT 1;"}})
	expectStatus(t, w, http.StatusUnprocessableEntity)
	contains(t, w.Body.String(), "judul wajib diisi")
	if f.e.count("SELECT count(*) FROM migrations") != 1 {
		t.Fatal("draft tidak valid tersimpan")
	}

	w = f.ed.post(f.base, url.Values{"schema": {"public"}, "desired_ddl": {desired}})
	expectStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), "CREATE TABLE public.zz_sd_app_books") {
		t.Fatal("schema diff masih mengusulkan tabel yang sudah ada")
	}

	logs := f.e.log.String()
	notContains(t, logs, "zz_sd_app_books", "CREATE TABLE", "desired_ddl", "password", "postgres://")
	notContains(t, logs, f.ed.session())
}

func TestSchemaDiffTargetDown(t *testing.T) {
	e := setup(t)
	dead := testutil.SeedConnection(t, e.db, func(in *dbconn.Input) { in.Port = closedPort; in.Password = secretPW })
	ed := e.member("sd_dead_editor", dead.ID, authz.LevelEdit)
	base := fmt.Sprintf("/databases/%d/schema-diff", dead.ID)

	w := ed.do("GET", base, nil)
	expectStatus(t, w, http.StatusServiceUnavailable)
	body := w.Body.String()
	contains(t, body, dead.Name, dbconn.KindRefused.UserMessage(), "Aplikasi tetap berjalan")
	notContains(t, body, "127.0.0.1", fmt.Sprint(closedPort), secretPW, "dial tcp", "connect:")
	if strings.Count(body, "<!doctype html>") != 1 {
		t.Fatal("respons error ditulis lebih dari sekali")
	}

	w = ed.post(base, url.Values{"schema": {"public"}, "desired_ddl": {"SELECT 1;"}})
	expectStatus(t, w, http.StatusServiceUnavailable)
	notContains(t, w.Body.String(), "127.0.0.1", secretPW)
	e.noSecretInLogs()
}
