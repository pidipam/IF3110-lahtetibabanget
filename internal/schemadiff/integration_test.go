package schemadiff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func targetPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testutil.RequireEnv(t, testutil.EnvTargetDB)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("konek ke target: %v", err)
	}
	return pool
}

func targetTable(t *testing.T, pool *pgxpool.Pool, name string) *Table {
	t.Helper()
	s, err := Load(context.Background(), pool, "public")
	if err != nil {
		t.Fatal(err)
	}
	return s.table(name)
}

func scratchSchemas(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT nspname FROM pg_namespace
		WHERE nspname LIKE 'zz\_schemadiff\_%' ORDER BY nspname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func dropSchema(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	if name == "" {
		return
	}
	if _, err := pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{name}.Sanitize()+" CASCADE"); err != nil {
		t.Fatal(err)
	}
}

func uniqueSchema(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

func TestSchemasAndLoad(t *testing.T) {
	pool := targetPool(t)
	ctx := context.Background()

	schemas, err := Schemas(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(schemas, "public") || !slices.Contains(schemas, "analytics") {
		t.Fatalf("schema pengguna tidak lengkap: %v", schemas)
	}
	for _, sys := range []string{"pg_catalog", "information_schema"} {
		if slices.Contains(schemas, sys) {
			t.Fatalf("schema sistem %q tidak boleh muncul", sys)
		}
	}

	pub, err := Load(ctx, pool, "public")
	if err != nil {
		t.Fatal(err)
	}
	if pub.Name != "public" || len(pub.Tables) < 4 || len(pub.Views) == 0 {
		t.Fatalf("hasil introspeksi public tidak lengkap: %+v", pub)
	}
	customers := pub.table("customers")
	if customers == nil {
		t.Fatal("tabel customers tidak ditemukan")
	}
	id := customers.column("id")
	if id == nil || id.Type != "integer" || !id.NotNull || !id.HasDefault ||
		id.Default != "nextval('customers_id_seq'::regclass)" {
		t.Fatalf("kolom customers.id salah: %+v", id)
	}
	if customers.constraint("customers_pkey") == nil || customers.index("customers_email_key") != nil {
		t.Fatal("constraint/index customers salah")
	}
	if customers.index("customers_city_idx") == nil {
		t.Fatal("index customers_city_idx tidak ditemukan")
	}
	if pub.sequence("customers_id_seq") == nil {
		t.Fatal("sequence customers_id_seq tidak ditemukan")
	}

	analytics, err := Load(ctx, pool, "analytics")
	if err != nil {
		t.Fatal(err)
	}
	if analytics.table("events") == nil {
		t.Fatal("tabel analytics.events tidak ditemukan")
	}

	for _, name := range []string{"pg_catalog", "pg_toast", "information_schema", "tidak_ada", "", strings.Repeat("x", 64)} {
		if _, err := Load(ctx, pool, name); !errors.Is(err, ErrSchemaNotFound) {
			t.Fatalf("Load(%q) harus ErrSchemaNotFound, dapat %v", name, err)
		}
	}
}

func TestDumpLoadDDLRoundTrip(t *testing.T) {
	pool := targetPool(t)
	ctx := context.Background()
	for _, name := range []string{"public", "analytics"} {
		t.Run(name, func(t *testing.T) {
			cur, err := Load(ctx, pool, name)
			if err != nil {
				t.Fatal(err)
			}
			dump := Dump(cur)
			if strings.TrimSpace(dump) == "" {
				t.Fatal("dump kosong")
			}
			want, err := LoadDDL(ctx, pool, name, dump)
			if err != nil {
				t.Fatalf("DDL hasil dump tidak dapat dijalankan: %v\n%s", err, dump)
			}
			res := Diff(cur, want, Options{Schema: name})
			if !res.Empty() {
				t.Fatalf("dump lalu diff ulang harus kosong, dapat %d perubahan:\n%+v\nSQL:\n%s", len(res.Changes), res.Changes, res.SQL)
			}
		})
	}
}

func TestLoadDDLRollsBackAndCleansScratch(t *testing.T) {
	pool := targetPool(t)
	ctx := context.Background()
	before := scratchSchemas(t, pool)

	out, err := LoadDDL(ctx, pool, "public", `
		CREATE TABLE zz_sd_preview (id integer PRIMARY KEY, note text);
		CREATE INDEX zz_sd_preview_note_idx ON zz_sd_preview (note);
		CREATE VIEW zz_sd_preview_v AS SELECT id FROM zz_sd_preview;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if out.table("zz_sd_preview") == nil || out.view("zz_sd_preview_v") == nil {
		t.Fatalf("DDL tidak terintrospeksi: %+v", out)
	}
	if out.Name != "public" {
		t.Fatalf("nama schema hasil harus nama target, dapat %q", out.Name)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.zz_sd_preview') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("DDL preview mengubah schema target (seharusnya rollback)")
	}
	after := scratchSchemas(t, pool)
	if len(after) != len(before) {
		t.Fatalf("schema sementara tertinggal: sebelum %v, sesudah %v", before, after)
	}

	_, err = LoadDDL(ctx, pool, "public", "CREATE TABLE zz_sd_broken (id tipe_tidak_ada);")
	var de *DDLError
	if !errors.As(err, &de) {
		t.Fatalf("DDL salah harus DDLError, dapat %T %v", err, err)
	}
	if strings.Contains(de.Error(), "postgres://") || strings.Contains(de.Error(), "password") {
		t.Fatalf("pesan error membocorkan koneksi: %q", de.Error())
	}
	if got := scratchSchemas(t, pool); len(got) != len(before) {
		t.Fatalf("schema sementara tertinggal setelah gagal: %v", got)
	}
}

func TestLoadDDLValidation(t *testing.T) {
	pool := targetPool(t)
	for _, tc := range []struct {
		name, schema, ddl string
		want              error
	}{
		{"schema kosong", "", "CREATE TABLE t (id int);", ErrSchemaNotFound},
		{"schema sistem", "pg_catalog", "CREATE TABLE t (id int);", ErrSchemaNotFound},
		{"ddl kosong", "public", "  \n\t", ErrEmptyDDL},
	} {
		if _, err := LoadDDL(context.Background(), pool, tc.schema, tc.ddl); !errors.Is(err, tc.want) {
			t.Fatalf("%s: dapat %v, want %v", tc.name, err, tc.want)
		}
	}
	_, err := LoadDDL(context.Background(), pool, "public", "CREATE TABLE t (id int);\x00")
	var de *DDLError
	if !errors.As(err, &de) {
		t.Fatalf("NUL harus ditolak sebagai DDLError, dapat %v", err)
	}
}

func TestGeneratedSQLApplies(t *testing.T) {
	pool := targetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema := uniqueSchema("zz_sd_it_")
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	ddl := `
		CREATE TYPE mood AS ENUM ('happy', 'sad');
		CREATE SEQUENCE ticket_seq;
		CREATE TABLE accounts (
			id bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
			email text NOT NULL,
			mood mood NOT NULL DEFAULT 'happy',
			CONSTRAINT accounts_pkey PRIMARY KEY (id),
			CONSTRAINT accounts_email_key UNIQUE (email)
		);
		CREATE INDEX accounts_mood_idx ON accounts (mood);
		CREATE TABLE orders (
			id serial NOT NULL,
			customer_id integer NOT NULL,
			CONSTRAINT orders_pkey PRIMARY KEY (id),
			CONSTRAINT orders_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES customers (id)
		);
		CREATE VIEW account_orders AS
			SELECT a.id AS account_id, count(o.id) AS total
			FROM accounts a LEFT JOIN orders o ON o.customer_id = a.id
			GROUP BY a.id;
	`
	cur, err := Load(ctx, pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	want, err := LoadDDL(ctx, pool, schema, ddl)
	if err != nil {
		t.Fatal(err)
	}
	res := Diff(cur, want, Options{Schema: schema})
	if res.Empty() || res.Statements == 0 {
		t.Fatalf("diff schema kosong harus menghasilkan statement: %+v", res)
	}
	// Foreign key ke public.customers harus tetap dapat diselesaikan saat SQL dijalankan.
	mustContain(t, res.SQL,
		"SET LOCAL search_path TO "+QuoteIdent(schema)+", public, pg_catalog;",
		"REFERENCES customers(id)",
		"CREATE VIEW "+QuoteIdent(schema)+".account_orders AS",
	)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, res.SQL); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("SQL hasil diff gagal dijalankan: %v\n%s", err, res.SQL)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	applied, err := Load(ctx, pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	check := Diff(want, applied, Options{Schema: schema})
	if !check.Empty() {
		t.Fatalf("schema hasil eksekusi berbeda dari kondisi yang diinginkan:\n%+v\nSQL:\n%s", check.Changes, check.SQL)
	}
	if applied.table("orders") == nil || applied.view("account_orders") == nil || applied.enum("mood") == nil {
		t.Fatalf("objek hasil eksekusi tidak lengkap: %+v", applied)
	}
}

func TestGeneratedSQLForCrossSchemaReferences(t *testing.T) {
	pool := targetPool(t)
	ctx := context.Background()
	cur, err := Load(ctx, pool, "analytics")
	if err != nil {
		t.Fatal(err)
	}
	res := Diff(&Schema{Name: "analytics"}, cur, Options{Schema: "analytics", IncludeDestructive: true})
	mustContain(t, res.SQL,
		"SET LOCAL search_path TO analytics, public, pg_catalog;",
		"CREATE TABLE analytics.events (",
	)
}

func TestLoadDDLDoesNotLeakScratchSchemaInDefinitions(t *testing.T) {
	pool := targetPool(t)
	ctx := context.Background()
	out, err := LoadDDL(ctx, pool, "public", `
		CREATE TABLE zz_sd_owner (id serial PRIMARY KEY);
		CREATE VIEW zz_sd_owner_v AS SELECT id FROM zz_sd_owner;
		CREATE TABLE zz_sd_child (
			id integer PRIMARY KEY,
			owner_id integer REFERENCES zz_sd_owner (id)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range out.Tables {
		for _, c := range tbl.Columns {
			if strings.Contains(c.Default, "zz_schemadiff_") || strings.Contains(c.Expression, "zz_schemadiff_") {
				t.Fatalf("nama schema sementara bocor ke default kolom: %+v", c)
			}
		}
		for _, con := range tbl.Constraints {
			if strings.Contains(con.Definition, "zz_schemadiff_") {
				t.Fatalf("nama schema sementara bocor ke constraint: %+v", con)
			}
		}
	}
	for _, v := range out.Views {
		if strings.Contains(v.Definition, "zz_schemadiff_") {
			t.Fatalf("nama schema sementara bocor ke view: %+v", v)
		}
	}
}
