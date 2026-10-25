package schemadiff

import (
	"strings"
	"testing"
)

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("hasil tidak memuat %q:\n%s", sub, s)
		}
	}
}

func mustNotContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Fatalf("hasil seharusnya tidak memuat %q:\n%s", sub, s)
		}
	}
}

func changesOf(res *Result, kind string) []Change {
	var out []Change
	for _, c := range res.Changes {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func TestDiffNoChanges(t *testing.T) {
	s := &Schema{
		Name:   "public",
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "id", Type: "integer", NotNull: true}}}},
		Views:  []View{{Name: "v", Definition: " SELECT t.id\n   FROM t;"}},
		Enums:  []Enum{{Name: "mood", Labels: []string{"ok", "bad"}}},
	}
	for _, opt := range []Options{{Schema: "public"}, {Schema: "public", IncludeDestructive: true}} {
		res := Diff(s, s, opt)
		if !res.Empty() || res.Statements != 0 || res.Skipped != 0 || res.SQL != "" {
			t.Fatalf("schema yang sama harus menghasilkan diff kosong: %+v", res)
		}
	}
}

func TestDiffAddTable(t *testing.T) {
	cur := &Schema{Name: "public"}
	want := &Schema{Name: "public", Tables: []Table{{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "integer", NotNull: true},
			{Name: "email", Type: "text", NotNull: true},
		},
		Constraints: []Constraint{{Name: "users_pkey", Type: ConPrimary, Definition: "PRIMARY KEY (id)"}},
		Indexes:     []Index{{Name: "users_email_idx", Body: "btree (email)"}},
	}}}
	res := Diff(cur, want, Options{Schema: "public"})
	if res.Empty() {
		t.Fatal("penambahan tabel harus terdeteksi")
	}
	if res.Statements != 2 || res.Skipped != 0 {
		t.Fatalf("statements=%d skipped=%d", res.Statements, res.Skipped)
	}
	cs := changesOf(res, "Tabel")
	if len(cs) != 1 || cs[0].Op != OpAdd || cs[0].Object != "users" {
		t.Fatalf("change tabel salah: %+v", cs)
	}
	mustContain(t, res.SQL,
		"SET LOCAL search_path TO public, pg_catalog;",
		"-- Tabel baru",
		"CREATE TABLE public.users (",
		"    id integer NOT NULL,",
		"    email text NOT NULL,",
		"    CONSTRAINT users_pkey PRIMARY KEY (id)",
		"CREATE INDEX users_email_idx ON public.users USING btree (email);",
	)
}

func TestDiffDropTableDestructive(t *testing.T) {
	cur := &Schema{Name: "public", Tables: []Table{{
		Name: "t", Columns: []Column{{Name: "id", Type: "integer"}},
	}}}
	want := &Schema{Name: "public"}

	res := Diff(cur, want, Options{Schema: "public"})
	if len(res.Changes) != 1 || !res.Changes[0].Destructive || res.Changes[0].Op != OpDrop {
		t.Fatalf("drop tabel harus ditandai destruktif: %+v", res.Changes)
	}
	if res.Statements != 0 || res.Skipped != 1 {
		t.Fatalf("statements=%d skipped=%d", res.Statements, res.Skipped)
	}
	mustContain(t, res.SQL, "-- DILEWATI (destruktif)", "-- DROP TABLE public.t;")
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "1 statement destruktif") {
		t.Fatalf("warning destruktif tidak ada: %+v", res.Warnings)
	}

	res = Diff(cur, want, Options{Schema: "public", IncludeDestructive: true})
	if res.Statements != 1 || res.Skipped != 0 {
		t.Fatalf("statements=%d skipped=%d", res.Statements, res.Skipped)
	}
	mustContain(t, res.SQL, "-- PERINGATAN: menghapus data.", "DROP TABLE public.t;")
	mustNotContain(t, res.SQL, "-- DILEWATI")
}

func TestDiffColumnChanges(t *testing.T) {
	cur := &Schema{Name: "public", Tables: []Table{{
		Name: "t",
		Columns: []Column{
			{Name: "id", Type: "integer", NotNull: true},
			{Name: "name", Type: "text"},
			{Name: "old", Type: "text"},
		},
	}}}
	want := &Schema{Name: "public", Tables: []Table{{
		Name: "t",
		Columns: []Column{
			{Name: "id", Type: "bigint", NotNull: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "created_at", Type: "timestamp with time zone", NotNull: true, HasDefault: true, Default: "now()"},
		},
	}}}
	res := Diff(cur, want, Options{Schema: "public"})
	if len(changesOf(res, "Kolom")) != 4 {
		t.Fatalf("perubahan kolom salah: %+v", changesOf(res, "Kolom"))
	}
	mustContain(t, res.SQL,
		"ALTER TABLE public.t ADD COLUMN created_at timestamp with time zone DEFAULT now() NOT NULL;",
		"ALTER TABLE public.t ALTER COLUMN id TYPE bigint USING id::bigint;",
		"ALTER TABLE public.t ALTER COLUMN name SET NOT NULL;",
		"-- ALTER TABLE public.t DROP COLUMN old;",
	)
	if res.Statements != 3 || res.Skipped != 1 {
		t.Fatalf("statements=%d skipped=%d", res.Statements, res.Skipped)
	}
	warns := strings.Join(res.Warnings, "\n")
	mustContain(t, warns, "Tipe kolom t.id diubah", "Kolom t.name menjadi NOT NULL", "1 statement destruktif")
}

func TestDiffConstraintsAndIndexes(t *testing.T) {
	cur := &Schema{Name: "public", Tables: []Table{{
		Name:    "t",
		Columns: []Column{{Name: "id", Type: "integer"}, {Name: "v", Type: "text"}},
		Constraints: []Constraint{
			{Name: "t_pkey", Type: ConPrimary, Definition: "PRIMARY KEY (id)"},
			{Name: "old_u", Type: ConUnique, Definition: "UNIQUE (v)"},
		},
		Indexes: []Index{{Name: "idx1", Body: "btree (v)"}},
	}}}
	want := &Schema{Name: "public", Tables: []Table{{
		Name:    "t",
		Columns: []Column{{Name: "id", Type: "integer"}, {Name: "v", Type: "text"}},
		Constraints: []Constraint{
			{Name: "t_pkey", Type: ConPrimary, Definition: "PRIMARY KEY (id)"},
			{Name: "new_c", Type: ConCheck, Definition: "CHECK ((id > 0))"},
		},
		Indexes: []Index{
			{Name: "idx1", Body: "btree (lower(v))"},
			{Name: "new_idx", Unique: true, Body: "btree (v)"},
		},
	}}}
	res := Diff(cur, want, Options{Schema: "public"})
	mustContain(t, res.SQL,
		"ALTER TABLE public.t DROP CONSTRAINT old_u;",
		"DROP INDEX public.idx1;",
		"ALTER TABLE public.t ADD CONSTRAINT new_c CHECK ((id > 0));",
		"CREATE INDEX idx1 ON public.t USING btree (lower(v));",
		"CREATE UNIQUE INDEX new_idx ON public.t USING btree (v);",
	)
	if res.Statements != 5 {
		t.Fatalf("statements=%d, SQL:\n%s", res.Statements, res.SQL)
	}
	if len(changesOf(res, "Constraint")) != 2 || len(changesOf(res, "Index")) != 2 {
		t.Fatalf("change constraint/index salah: %+v", res.Changes)
	}
}

func TestDiffForeignKey(t *testing.T) {
	cur := &Schema{Name: "public", Tables: []Table{
		{Name: "parent", Columns: []Column{{Name: "id", Type: "integer"}}},
		{Name: "child", Columns: []Column{{Name: "parent_id", Type: "integer"}}, Constraints: []Constraint{
			{Name: "child_parent_fkey", Type: ConForeign, Definition: "FOREIGN KEY (parent_id) REFERENCES parent(id)"},
		}},
	}}
	want := &Schema{Name: "public", Tables: []Table{
		{Name: "parent", Columns: []Column{{Name: "id", Type: "integer"}}},
		{Name: "child", Columns: []Column{{Name: "parent_id", Type: "integer"}}, Constraints: []Constraint{
			{Name: "child_parent_fkey", Type: ConForeign, Definition: "FOREIGN KEY (parent_id) REFERENCES parent(id) ON DELETE CASCADE"},
		}},
	}}
	res := Diff(cur, want, Options{Schema: "public"})
	mustContain(t, res.SQL,
		"-- Hapus foreign key",
		"ALTER TABLE public.child DROP CONSTRAINT child_parent_fkey;",
		"-- Foreign key",
		"ALTER TABLE public.child ADD CONSTRAINT child_parent_fkey FOREIGN KEY (parent_id) REFERENCES parent(id) ON DELETE CASCADE;",
	)
	if len(changesOf(res, "Constraint")) != 1 || changesOf(res, "Constraint")[0].Op != OpAlter {
		t.Fatalf("perubahan FK salah: %+v", changesOf(res, "Constraint"))
	}
	if res.Statements != 2 {
		t.Fatalf("statements=%d", res.Statements)
	}
}

func TestDiffViews(t *testing.T) {
	oldDef := " SELECT t.a\n   FROM t;"
	newDef := " SELECT t.a,\n    t.a AS b\n   FROM t;"
	cur := &Schema{Name: "public",
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "integer"}}}},
		Views: []View{
			{Name: "v", Definition: oldDef},
			{Name: "gone", Definition: " SELECT 1;"},
		},
	}
	want := &Schema{Name: "public",
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "integer"}}}},
		Views: []View{
			{Name: "v", Definition: newDef},
			{Name: "added", Definition: " SELECT 1 AS one;"},
		},
	}
	res := Diff(cur, want, Options{Schema: "public"})
	mustContain(t, res.SQL,
		"-- Hapus view yang dihapus atau dibuat ulang",
		"DROP VIEW public.gone;",
		"DROP VIEW public.v;",
		"CREATE VIEW public.added AS\n SELECT 1 AS one;",
		"CREATE VIEW public.v AS\n SELECT t.a,",
	)
	ops := map[string]Op{}
	for _, c := range changesOf(res, "View") {
		ops[c.Object] = c.Op
	}
	if ops["gone"] != OpDrop || ops["added"] != OpAdd || ops["v"] != OpAlter {
		t.Fatalf("change view salah: %+v", res.Changes)
	}
}

func TestDiffViewRecreatedWhenColumnTypeChanges(t *testing.T) {
	def := " SELECT t.a\n   FROM t;"
	cur := &Schema{Name: "public",
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "integer"}}}},
		Views:  []View{{Name: "v", Definition: def}},
	}
	want := &Schema{Name: "public",
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "bigint"}}}},
		Views:  []View{{Name: "v", Definition: def}},
	}
	res := Diff(cur, want, Options{Schema: "public"})
	mustContain(t, res.SQL, "DROP VIEW public.v;", "CREATE VIEW public.v AS")
	cs := changesOf(res, "View")
	if len(cs) != 1 || !strings.Contains(cs[0].Detail, "dibuat ulang") {
		t.Fatalf("view yang bergantung harus dibuat ulang: %+v", cs)
	}
}

func TestDiffEnums(t *testing.T) {
	cur := &Schema{Name: "public", Enums: []Enum{
		{Name: "status", Labels: []string{"a", "b"}},
		{Name: "mood", Labels: []string{"x", "y"}},
		{Name: "old", Labels: []string{"z"}},
	}}
	want := &Schema{Name: "public", Enums: []Enum{
		{Name: "status", Labels: []string{"a", "b", "c"}},
		{Name: "mood", Labels: []string{"y", "x"}},
		{Name: "color", Labels: []string{"red", "green"}},
	}}
	res := Diff(cur, want, Options{Schema: "public"})
	mustContain(t, res.SQL,
		"CREATE TYPE public.color AS ENUM ('red', 'green');",
		"ALTER TYPE public.status ADD VALUE 'c' AFTER 'b';",
		"-- PERLU DITULIS MANUAL:",
		"-- DROP TYPE public.old;",
	)
	if res.Statements != 2 || res.Skipped != 1 {
		t.Fatalf("statements=%d skipped=%d, SQL:\n%s", res.Statements, res.Skipped, res.SQL)
	}
	warns := strings.Join(res.Warnings, "\n")
	mustContain(t, warns, "Nilai baru enum status", "Nilai enum mood dihapus", "1 statement destruktif")
}

func TestDiffSequencesAndSerial(t *testing.T) {
	cur := &Schema{Name: "public", Tables: []Table{{
		Name: "existing", Columns: []Column{{Name: "id", Type: "integer", NotNull: true}},
	}}}
	want := &Schema{Name: "public",
		Tables: []Table{
			{Name: "existing", Columns: []Column{{Name: "id", Type: "integer", NotNull: true}}},
			{Name: "t", Columns: []Column{{
				Name: "id", Type: "integer", NotNull: true, HasDefault: true,
				Default: "nextval('t_id_seq'::regclass)",
			}}},
		},
		Sequences: []Sequence{
			{Name: "counter"},
			{Name: "t_id_seq", OwnerTable: "t", OwnerColumn: "id"},
		},
	}
	res := Diff(cur, want, Options{Schema: "public"})
	mustContain(t, res.SQL,
		"CREATE SEQUENCE IF NOT EXISTS public.counter;",
		"CREATE TABLE public.t (",
		"    id serial NOT NULL",
	)
	mustNotContain(t, res.SQL, "CREATE SEQUENCE IF NOT EXISTS public.t_id_seq")
	if len(changesOf(res, "Sequence")) != 1 || changesOf(res, "Sequence")[0].Object != "counter" {
		t.Fatalf("change sequence salah: %+v", changesOf(res, "Sequence"))
	}

	// Sequence yang dihapus hanya menjadi komentar.
	drop := Diff(want, cur, Options{Schema: "public"})
	mustContain(t, drop.SQL, "-- DROP SEQUENCE public.counter;")
	if drop.Skipped == 0 {
		t.Fatal("drop sequence harus dilewati")
	}
	active := Diff(want, cur, Options{Schema: "public", IncludeDestructive: true})
	mustContain(t, active.SQL, "DROP SEQUENCE public.counter;")
}

func TestDiffOrderedPhasesAndQuoting(t *testing.T) {
	cur := &Schema{Name: "Sales Data", Tables: []Table{{
		Name: "Order", Columns: []Column{{Name: "User", Type: "text"}},
		Constraints: []Constraint{{Name: "Order pkey", Type: ConPrimary, Definition: `PRIMARY KEY ("User")`}},
	}}}
	want := &Schema{Name: "Sales Data"}
	res := Diff(cur, want, Options{Schema: "Sales Data", IncludeDestructive: true})
	mustContain(t, res.SQL,
		`SET LOCAL search_path TO "Sales Data", public, pg_catalog;`,
		`DROP TABLE "Sales Data"."Order";`,
	)
}

func TestDiffManualGeneratedColumn(t *testing.T) {
	cur := &Schema{Name: "public", Tables: []Table{{
		Name:    "t",
		Columns: []Column{{Name: "total", Type: "integer", Generated: "s", Expression: "(a + b)"}},
	}}}
	want := &Schema{Name: "public", Tables: []Table{{
		Name:    "t",
		Columns: []Column{{Name: "total", Type: "integer", Generated: "s", Expression: "(a - b)"}},
	}}}
	res := Diff(cur, want, Options{Schema: "public"})
	mustContain(t, res.SQL, "-- PERLU DITULIS MANUAL:", "-- ALTER TABLE public.t DROP COLUMN total;")
	if res.Statements != 0 {
		t.Fatalf("statement manual tidak boleh dihitung aktif: %d", res.Statements)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("warning kolom generated harus ada")
	}
}

func TestDump(t *testing.T) {
	s := &Schema{
		Name:  "public",
		Enums: []Enum{{Name: "color", Labels: []string{"red", "green"}}},
		Tables: []Table{{
			Name: "t",
			Columns: []Column{
				{Name: "id", Type: "integer", NotNull: true},
				{Name: "c", Type: "color", NotNull: true, HasDefault: true, Default: "'red'::color"},
			},
			Constraints: []Constraint{{Name: "t_pkey", Type: ConPrimary, Definition: "PRIMARY KEY (id)"}},
		}},
		Sequences: []Sequence{{Name: "counter"}},
		Views:     []View{{Name: "v", Definition: " SELECT t.id\n   FROM t;"}},
	}
	dump := Dump(s)
	mustContain(t, dump,
		"CREATE TYPE color AS ENUM ('red', 'green');",
		"CREATE SEQUENCE IF NOT EXISTS counter;",
		"CREATE TABLE t (",
		"    id integer NOT NULL,",
		"    c color DEFAULT 'red'::color NOT NULL,",
		"    CONSTRAINT t_pkey PRIMARY KEY (id)",
		"CREATE VIEW v AS\n SELECT t.id\n   FROM t;",
	)
	mustNotContain(t, dump, "SET LOCAL search_path", "-- Dihasilkan otomatis")
	if !strings.HasPrefix(dump, "CREATE TYPE color") {
		t.Fatalf("dump harus dimulai dengan objek prelude:\n%s", dump)
	}
}
