package schemadiff

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Op string

const (
	OpAdd   Op = "add"
	OpDrop  Op = "drop"
	OpAlter Op = "alter"
)

func (o Op) Label() string {
	switch o {
	case OpAdd:
		return "Tambah"
	case OpDrop:
		return "Hapus"
	}
	return "Ubah"
}

// Change adalah satu baris ringkasan perbedaan untuk ditampilkan ke pengguna.
type Change struct {
	Op          Op
	Kind        string // Tabel, Kolom, Constraint, Index, View, Enum, Sequence
	Object      string
	Detail      string
	Destructive bool // dapat menghilangkan data
}

type Result struct {
	Changes  []Change
	Warnings []string
	SQL      string
	// Statements adalah jumlah statement aktif di SQL (bukan komentar).
	Statements int
	// Skipped adalah jumlah statement destruktif yang dijadikan komentar.
	Skipped int
}

func (r *Result) Empty() bool { return len(r.Changes) == 0 }

type Options struct {
	// Schema adalah schema target. Nama objek di-qualify dengan schema ini dan
	// SQL diawali SET LOCAL search_path (schema, public, pg_catalog) agar referensi
	// tanpa schema (tipe, sequence di default, tabel di foreign key dan view)
	// mengarah ke schema yang sama seperti saat introspeksi.
	Schema string
	// IncludeDestructive menyertakan DROP TABLE/COLUMN/TYPE/SEQUENCE sebagai SQL aktif.
	// Bila false statement tersebut tetap ditulis tetapi sebagai komentar.
	IncludeDestructive bool
	// Comment ditulis sebagai komentar di awal SQL.
	Comment string

	dump bool
}

const (
	phPrelude = iota
	phViewDrop
	phFKDrop
	phConDrop
	phTableDrop
	phTableCreate
	phColumns
	phConAdd
	phFKAdd
	phViewCreate
	phCleanup
	numPhases
)

var phaseTitle = [numPhases]string{
	"Tipe enum dan sequence",
	"Hapus view yang dihapus atau dibuat ulang",
	"Hapus foreign key",
	"Hapus constraint dan index",
	"Hapus tabel",
	"Tabel baru",
	"Perubahan kolom",
	"Constraint dan index baru",
	"Foreign key",
	"View",
	"Hapus tipe enum dan sequence",
}

type stmt struct {
	sql         string
	destructive bool
	manual      bool   // tidak dapat dibuat otomatis; ditulis sebagai komentar
	note        string // komentar tambahan di atas statement
}

type gen struct {
	opt     Options
	cur     *Schema
	want    *Schema
	res     *Result
	out     [numPhases][]stmt
	serial  map[string]bool // sequence yang akan dibuat lewat tipe serial
	touched map[string]bool // tabel yang kolomnya berubah tipe atau dihapus (view perlu dibuat ulang)
}

// Diff menghasilkan perbedaan dan SQL untuk mengubah cur menjadi want.
func Diff(cur, want *Schema, opt Options) *Result {
	if cur == nil {
		cur = &Schema{}
	}
	if want == nil {
		want = &Schema{}
	}
	g := &gen{opt: opt, cur: cur, want: want, res: &Result{}, serial: map[string]bool{}, touched: map[string]bool{}}
	g.enums()
	g.tables()
	g.sequences()
	g.views()
	g.render()
	return g.res
}

// Dump menulis schema sebagai DDL tanpa nama schema, cocok sebagai titik awal
// "schema yang diinginkan" yang kemudian diedit pengguna.
func Dump(s *Schema) string {
	return Diff(&Schema{}, s, Options{IncludeDestructive: true, dump: true}).SQL
}

func (g *gen) obj(name string) string {
	if g.opt.Schema == "" {
		return QuoteIdent(name)
	}
	return QuoteIdent(g.opt.Schema) + "." + QuoteIdent(name)
}

func (g *gen) emit(phase int, s stmt) { g.out[phase] = append(g.out[phase], s) }

func (g *gen) change(c Change) { g.res.Changes = append(g.res.Changes, c) }

func (g *gen) warn(format string, args ...any) {
	g.res.Warnings = append(g.res.Warnings, fmt.Sprintf(format, args...))
}

// ---- enum ----

func (g *gen) enums() {
	for _, e := range g.want.Enums {
		c := g.cur.enum(e.Name)
		if c == nil {
			g.change(Change{Op: OpAdd, Kind: "Enum", Object: e.Name, Detail: strings.Join(e.Labels, ", ")})
			g.emit(phPrelude, stmt{sql: fmt.Sprintf("CREATE TYPE %s AS ENUM (%s);", g.obj(e.Name), literalList(e.Labels))})
			continue
		}
		if slices.Equal(c.Labels, e.Labels) {
			continue
		}
		if !isSubsequence(c.Labels, e.Labels) {
			g.change(Change{Op: OpAlter, Kind: "Enum", Object: e.Name, Detail: "nilai dihapus atau diurutkan ulang (perlu ditulis manual)"})
			g.warn("Nilai enum %s dihapus atau diurutkan ulang. PostgreSQL tidak dapat melakukannya langsung; buat tipe baru lalu ubah kolom yang memakainya secara manual.", e.Name)
			g.emit(phPrelude, stmt{manual: true, sql: fmt.Sprintf("-- nilai sekarang: %s\n-- nilai diinginkan: %s", literalList(c.Labels), literalList(e.Labels))})
			continue
		}
		var added []string
		for i, l := range e.Labels {
			if slices.Contains(c.Labels, l) {
				continue
			}
			added = append(added, l)
			pos := "BEFORE " + quoteLiteral(c.Labels[0])
			if i > 0 {
				pos = "AFTER " + quoteLiteral(e.Labels[i-1])
			}
			g.emit(phPrelude, stmt{sql: fmt.Sprintf("ALTER TYPE %s ADD VALUE %s %s;", g.obj(e.Name), quoteLiteral(l), pos)})
		}
		g.change(Change{Op: OpAlter, Kind: "Enum", Object: e.Name, Detail: "nilai baru: " + strings.Join(added, ", ")})
		g.warn("Nilai baru enum %s belum dapat dipakai (mis. sebagai default atau di data) dalam migration yang sama. Bila perlu, pisahkan menjadi dua migration.", e.Name)
	}
	for _, c := range g.cur.Enums {
		if g.want.enum(c.Name) == nil {
			g.change(Change{Op: OpDrop, Kind: "Enum", Object: c.Name, Destructive: true})
			g.emit(phCleanup, stmt{destructive: true, sql: fmt.Sprintf("DROP TYPE %s;", g.obj(c.Name))})
		}
	}
}

func literalList(ls []string) string {
	q := make([]string, len(ls))
	for i, l := range ls {
		q[i] = quoteLiteral(l)
	}
	return strings.Join(q, ", ")
}

// isSubsequence: semua elemen a muncul di b dengan urutan yang sama.
func isSubsequence(a, b []string) bool {
	j := 0
	for _, x := range b {
		if j < len(a) && a[j] == x {
			j++
		}
	}
	return j == len(a)
}

// ---- tabel ----

func (g *gen) tables() {
	for i := range g.want.Tables {
		t := &g.want.Tables[i]
		if c := g.cur.table(t.Name); c != nil {
			g.alterTable(c, t)
		} else {
			g.createTable(t)
		}
	}
	for _, c := range g.cur.Tables {
		if g.want.table(c.Name) == nil {
			g.touched[c.Name] = true
			g.change(Change{Op: OpDrop, Kind: "Tabel", Object: c.Name, Detail: fmt.Sprintf("%d kolom", len(c.Columns)), Destructive: true})
			g.emit(phTableDrop, stmt{destructive: true, sql: fmt.Sprintf("DROP TABLE %s;", g.obj(c.Name))})
		}
	}
}

var nextvalRe = regexp.MustCompile(`^nextval\('([^']+)'::regclass\)$`)

var serialTypes = map[string]string{"integer": "serial", "bigint": "bigserial", "smallint": "smallserial"}

// serialType mengembalikan serial/bigserial/smallserial bila kolom baru dapat ditulis
// sebagai serial: default nextval ke sequence bernama <tabel>_<kolom>_seq yang dimiliki
// kolom tersebut dan belum ada di schema saat ini.
func (g *gen) serialType(table string, c *Column) string {
	st, ok := serialTypes[c.Type]
	if !ok || !c.NotNull || !c.HasDefault || c.Identity != "" || c.Generated != "" {
		return ""
	}
	seq := table + "_" + c.Name + "_seq"
	m := nextvalRe.FindStringSubmatch(c.Default)
	if m == nil || m[1] != QuoteIdent(seq) {
		return ""
	}
	ws := g.want.sequence(seq)
	if ws == nil || ws.OwnerTable != table || ws.OwnerColumn != c.Name || g.cur.sequence(seq) != nil {
		return ""
	}
	g.serial[seq] = true
	return st
}

// columnDef menulis definisi kolom untuk CREATE TABLE dan ADD COLUMN.
func (g *gen) columnDef(table string, c *Column) string {
	parts := []string{QuoteIdent(c.Name)}
	serial := g.serialType(table, c)
	if serial != "" {
		parts = append(parts, serial)
	} else {
		parts = append(parts, c.Type)
	}
	switch {
	case c.Generated != "":
		parts = append(parts, generatedClause(c))
	case c.Identity != "":
		parts = append(parts, identityClause(c.Identity))
	case c.HasDefault && serial == "":
		parts = append(parts, "DEFAULT "+c.Default)
	}
	if c.NotNull {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " ")
}

func generatedClause(c *Column) string {
	kind := "STORED"
	if c.Generated == "v" {
		kind = "VIRTUAL"
	}
	return "GENERATED ALWAYS AS (" + c.Expression + ") " + kind
}

func identityClause(kind string) string {
	if kind == "a" {
		return "GENERATED ALWAYS AS IDENTITY"
	}
	return "GENERATED BY DEFAULT AS IDENTITY"
}

func (g *gen) createTable(t *Table) {
	lines := make([]string, 0, len(t.Columns)+len(t.Constraints))
	for i := range t.Columns {
		lines = append(lines, "    "+g.columnDef(t.Name, &t.Columns[i]))
	}
	for _, con := range t.Constraints {
		if con.Type == ConForeign {
			g.emit(phFKAdd, stmt{sql: g.addConstraint(t.Name, con)})
			continue
		}
		lines = append(lines, fmt.Sprintf("    CONSTRAINT %s %s", QuoteIdent(con.Name), con.Definition))
	}
	g.emit(phTableCreate, stmt{sql: fmt.Sprintf("CREATE TABLE %s (\n%s\n);", g.obj(t.Name), strings.Join(lines, ",\n"))})
	for _, idx := range t.Indexes {
		g.emit(phConAdd, stmt{sql: g.createIndex(t.Name, idx)})
	}
	detail := fmt.Sprintf("%d kolom", len(t.Columns))
	if n := len(t.Constraints); n > 0 {
		detail += fmt.Sprintf(", %d constraint", n)
	}
	if n := len(t.Indexes); n > 0 {
		detail += fmt.Sprintf(", %d index", n)
	}
	g.change(Change{Op: OpAdd, Kind: "Tabel", Object: t.Name, Detail: detail})
}

func (g *gen) addConstraint(table string, con Constraint) string {
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;", g.obj(table), QuoteIdent(con.Name), con.Definition)
}

func (g *gen) createIndex(table string, idx Index) string {
	u := ""
	if idx.Unique {
		u = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s USING %s;", u, QuoteIdent(idx.Name), g.obj(table), idx.Body)
}

func (g *gen) alterTable(c, w *Table) {
	tbl := g.obj(w.Name)
	var adds, alters, drops []stmt
	for i := range w.Columns {
		wc := &w.Columns[i]
		cc := c.column(wc.Name)
		if cc == nil {
			def := g.columnDef(w.Name, wc)
			adds = append(adds, stmt{sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", tbl, def)})
			g.change(Change{Op: OpAdd, Kind: "Kolom", Object: w.Name + "." + wc.Name, Detail: wc.Type})
			if wc.NotNull && !wc.HasDefault && wc.Identity == "" && wc.Generated == "" {
				g.warn("Kolom %s.%s ditambahkan sebagai NOT NULL tanpa default; migration akan gagal bila tabel %s sudah berisi data.", w.Name, wc.Name, w.Name)
			}
			continue
		}
		alters = append(alters, g.alterColumn(w.Name, cc, wc)...)
	}
	for _, cc := range c.Columns {
		if w.column(cc.Name) == nil {
			g.touched[w.Name] = true
			drops = append(drops, stmt{destructive: true, sql: fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tbl, QuoteIdent(cc.Name))})
			g.change(Change{Op: OpDrop, Kind: "Kolom", Object: w.Name + "." + cc.Name, Detail: cc.Type, Destructive: true})
		}
	}
	g.out[phColumns] = append(g.out[phColumns], adds...)
	g.out[phColumns] = append(g.out[phColumns], alters...)
	g.out[phColumns] = append(g.out[phColumns], drops...)

	for _, cc := range c.Constraints {
		wc := w.constraint(cc.Name)
		if wc != nil && wc.Definition == cc.Definition && wc.Type == cc.Type {
			continue
		}
		ph := phConDrop
		if cc.Type == ConForeign {
			ph = phFKDrop
		}
		g.emit(ph, stmt{sql: fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s;", tbl, QuoteIdent(cc.Name))})
		if wc == nil {
			g.change(Change{Op: OpDrop, Kind: "Constraint", Object: w.Name + "." + cc.Name, Detail: cc.Definition})
		}
	}
	for _, wc := range w.Constraints {
		cc := c.constraint(wc.Name)
		if cc != nil && wc.Definition == cc.Definition && wc.Type == cc.Type {
			continue
		}
		ph := phConAdd
		if wc.Type == ConForeign {
			ph = phFKAdd
		}
		g.emit(ph, stmt{sql: g.addConstraint(w.Name, wc)})
		if cc == nil {
			g.change(Change{Op: OpAdd, Kind: "Constraint", Object: w.Name + "." + wc.Name, Detail: wc.Definition})
		} else {
			g.change(Change{Op: OpAlter, Kind: "Constraint", Object: w.Name + "." + wc.Name, Detail: cc.Definition + " → " + wc.Definition})
		}
	}

	for _, ci := range c.Indexes {
		wi := w.index(ci.Name)
		if wi != nil && *wi == ci {
			continue
		}
		g.emit(phConDrop, stmt{sql: fmt.Sprintf("DROP INDEX %s;", g.obj(ci.Name))})
		if wi == nil {
			g.change(Change{Op: OpDrop, Kind: "Index", Object: ci.Name, Detail: "pada " + w.Name})
		}
	}
	for _, wi := range w.Indexes {
		ci := c.index(wi.Name)
		if ci != nil && *ci == wi {
			continue
		}
		g.emit(phConAdd, stmt{sql: g.createIndex(w.Name, wi)})
		op, detail := OpAdd, "pada "+w.Name+": "+wi.Body
		if ci != nil {
			op, detail = OpAlter, "pada "+w.Name+": "+ci.Body+" → "+wi.Body
		}
		g.change(Change{Op: op, Kind: "Index", Object: wi.Name, Detail: detail})
	}
}

func (g *gen) alterColumn(table string, c, w *Column) []stmt {
	prefix := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s ", g.obj(table), QuoteIdent(w.Name))
	name := table + "." + w.Name
	var out []stmt
	var details []string
	add := func(s string) { out = append(out, stmt{sql: prefix + s + ";"}) }

	if c.Generated != w.Generated || c.Expression != w.Expression {
		if c.Generated != "" && w.Generated == "" {
			add("DROP EXPRESSION")
			details = append(details, "bukan lagi kolom generated")
			c = &Column{Name: c.Name, Type: c.Type, NotNull: c.NotNull}
		} else {
			g.change(Change{Op: OpAlter, Kind: "Kolom", Object: name, Detail: "ekspresi generated berubah (perlu ditulis manual)"})
			g.warn("Ekspresi kolom generated %s berubah. Kolom generated tidak dapat diubah langsung; hapus lalu tambahkan ulang kolom tersebut secara manual.", name)
			return []stmt{{manual: true, sql: fmt.Sprintf("-- ALTER TABLE %s DROP COLUMN %s;\n-- ALTER TABLE %s ADD COLUMN %s;",
				g.obj(table), QuoteIdent(w.Name), g.obj(table), g.columnDef(table, w))}}
		}
	}

	if c.Identity != "" && w.Identity == "" {
		add("DROP IDENTITY")
		details = append(details, "identity dihapus")
	}
	hasDefault, def := c.HasDefault, c.Default
	if c.Type != w.Type {
		if hasDefault {
			add("DROP DEFAULT")
			hasDefault, def = false, ""
		}
		add(fmt.Sprintf("TYPE %s USING %s::%s", w.Type, QuoteIdent(w.Name), w.Type))
		details = append(details, "tipe "+c.Type+" → "+w.Type)
		g.touched[table] = true
		g.warn("Tipe kolom %s diubah dari %s ke %s. Periksa apakah semua data dapat dikonversi; ubah klausa USING bila perlu.", name, c.Type, w.Type)
	}
	switch {
	case w.HasDefault && (!hasDefault || def != w.Default):
		add("SET DEFAULT " + w.Default)
		if !(c.HasDefault && c.Default == w.Default) {
			details = append(details, "default "+w.Default)
		}
	case !w.HasDefault && hasDefault:
		add("DROP DEFAULT")
		details = append(details, "default dihapus")
	}
	if c.NotNull != w.NotNull {
		if w.NotNull {
			add("SET NOT NULL")
			details = append(details, "menjadi NOT NULL")
			g.warn("Kolom %s menjadi NOT NULL; migration akan gagal bila masih ada baris bernilai NULL.", name)
		} else {
			add("DROP NOT NULL")
			details = append(details, "boleh NULL")
		}
	}
	switch {
	case c.Identity == "" && w.Identity != "":
		add("ADD " + identityClause(w.Identity))
		details = append(details, "menjadi identity")
	case c.Identity != "" && w.Identity != "" && c.Identity != w.Identity:
		kind := "BY DEFAULT"
		if w.Identity == "a" {
			kind = "ALWAYS"
		}
		add("SET GENERATED " + kind)
		details = append(details, "identity "+kind)
	}
	if len(out) > 0 {
		g.change(Change{Op: OpAlter, Kind: "Kolom", Object: name, Detail: strings.Join(details, "; ")})
	}
	return out
}

// ---- sequence ----

func (g *gen) sequences() {
	for _, s := range g.want.Sequences {
		if g.cur.sequence(s.Name) != nil || g.serial[s.Name] {
			continue
		}
		g.emit(phPrelude, stmt{sql: fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s;", g.obj(s.Name))})
		if s.OwnerTable != "" {
			g.emit(phConAdd, stmt{sql: fmt.Sprintf("ALTER SEQUENCE %s OWNED BY %s.%s;", g.obj(s.Name), g.obj(s.OwnerTable), QuoteIdent(s.OwnerColumn))})
		}
		g.change(Change{Op: OpAdd, Kind: "Sequence", Object: s.Name})
	}
	for _, s := range g.cur.Sequences {
		if g.want.sequence(s.Name) != nil {
			continue
		}
		if s.OwnerTable != "" {
			// Ikut terhapus bersama tabel/kolom pemiliknya.
			wt := g.want.table(s.OwnerTable)
			if wt == nil || wt.column(s.OwnerColumn) == nil {
				continue
			}
		}
		g.change(Change{Op: OpDrop, Kind: "Sequence", Object: s.Name, Destructive: true})
		g.emit(phCleanup, stmt{destructive: true, sql: fmt.Sprintf("DROP SEQUENCE %s;", g.obj(s.Name))})
	}
}

// ---- view ----

func (g *gen) views() {
	recreate := map[string]bool{}
	for _, w := range g.want.Views {
		if c := g.cur.view(w.Name); c != nil && c.Definition != w.Definition {
			recreate[w.Name] = true
		}
	}
	// View yang tidak berubah tetapi merujuk tabel yang kolomnya berubah tipe/dihapus
	// atau merujuk view lain yang dibuat ulang ikut dibuat ulang, karena PostgreSQL
	// menolak perubahan tersebut selama view masih bergantung padanya.
	for changed := true; changed; {
		changed = false
		for _, w := range g.want.Views {
			c := g.cur.view(w.Name)
			if c == nil || recreate[w.Name] {
				continue
			}
			for name := range g.touched {
				if mentions(c.Definition, name) {
					recreate[w.Name], changed = true, true
					break
				}
			}
			if recreate[w.Name] {
				continue
			}
			for name := range recreate {
				if mentions(c.Definition, name) {
					recreate[w.Name], changed = true, true
					break
				}
			}
			if !recreate[w.Name] {
				for _, cv := range g.cur.Views {
					if g.want.view(cv.Name) == nil && mentions(c.Definition, cv.Name) {
						recreate[w.Name], changed = true, true
						break
					}
				}
			}
		}
	}
	// Hapus dengan urutan terbalik dari urutan pembuatan supaya view yang bergantung dihapus dulu.
	for i := len(g.cur.Views) - 1; i >= 0; i-- {
		c := g.cur.Views[i]
		switch w := g.want.view(c.Name); {
		case w == nil:
			g.emit(phViewDrop, stmt{sql: fmt.Sprintf("DROP VIEW %s;", g.obj(c.Name))})
			g.change(Change{Op: OpDrop, Kind: "View", Object: c.Name})
		case recreate[c.Name]:
			g.emit(phViewDrop, stmt{sql: fmt.Sprintf("DROP VIEW %s;", g.obj(c.Name))})
		}
	}
	for _, w := range g.want.Views {
		c := g.cur.view(w.Name)
		switch {
		case c == nil:
			g.change(Change{Op: OpAdd, Kind: "View", Object: w.Name})
		case c.Definition != w.Definition:
			g.change(Change{Op: OpAlter, Kind: "View", Object: w.Name, Detail: "definisi berubah"})
		case recreate[w.Name]:
			g.change(Change{Op: OpAlter, Kind: "View", Object: w.Name, Detail: "dibuat ulang karena objek yang dirujuk berubah"})
		default:
			continue
		}
		// pg_get_viewdef sudah berformat (baris pertama diawali spasi); cukup buang ";" di akhir.
		def := strings.TrimRight(strings.TrimRight(w.Definition, " \t\r\n"), ";")
		g.emit(phViewCreate, stmt{sql: fmt.Sprintf("CREATE VIEW %s AS\n%s;", g.obj(w.Name), def)})
	}
}

func isIdentChar(b byte) bool {
	return b == '_' || b == '$' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

// mentions memeriksa apakah definisi menyebut nama objek sebagai identifier utuh.
func mentions(def, name string) bool {
	ident := QuoteIdent(name)
	for i := 0; ; {
		j := strings.Index(def[i:], ident)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(ident)
		if (start == 0 || !isIdentChar(def[start-1])) && (end == len(def) || !isIdentChar(def[end])) {
			return true
		}
		i = start + 1
	}
}

// ---- render ----

// searchPath mengembalikan search_path yang dipakai saat introspeksi: schema
// target, public (tempat objek lintas schema biasanya berada), lalu pg_catalog.
// public ikut disertakan agar referensi tanpa schema pada foreign key atau view
// yang menunjuk objek public tetap dapat diselesaikan saat SQL dijalankan.
func (g *gen) searchPath() string {
	names := []string{g.opt.Schema, "public", "pg_catalog"}
	seen := map[string]bool{}
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		quoted = append(quoted, QuoteIdent(n))
	}
	return strings.Join(quoted, ", ")
}

func (g *gen) render() {
	total := 0
	for _, ph := range g.out {
		total += len(ph)
	}
	if total == 0 {
		return
	}
	var b strings.Builder
	if !g.opt.dump {
		for _, l := range strings.Split(strings.TrimSpace(g.opt.Comment), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				b.WriteString("-- " + l + "\n")
			}
		}
		b.WriteString("-- Dihasilkan otomatis oleh schema diff. Tinjau dan sesuaikan sebelum dijalankan.\n")
		if g.opt.Schema != "" {
			b.WriteString("SET LOCAL search_path TO " + g.searchPath() + ";\n")
		}
	}
	first := true
	for p, list := range g.out {
		if len(list) == 0 {
			continue
		}
		if !g.opt.dump {
			b.WriteString("\n-- " + phaseTitle[p] + "\n")
		} else if !first {
			b.WriteString("\n")
		}
		first = false
		for _, s := range list {
			active := !s.manual && (!s.destructive || g.opt.IncludeDestructive || g.opt.dump)
			if active {
				g.res.Statements++
			}
			switch {
			case s.manual:
				b.WriteString("-- PERLU DITULIS MANUAL:\n" + s.sql + "\n")
			case s.destructive && !g.opt.IncludeDestructive:
				g.res.Skipped++
				b.WriteString("-- DILEWATI (destruktif). Hapus tanda komentar untuk menjalankan:\n")
				for _, l := range strings.Split(s.sql, "\n") {
					b.WriteString("-- " + l + "\n")
				}
			case s.destructive && !g.opt.dump:
				b.WriteString("-- PERINGATAN: menghapus data.\n" + s.sql + "\n")
			default:
				b.WriteString(s.sql + "\n")
			}
		}
	}
	if g.res.Skipped > 0 {
		g.warn("%d statement destruktif (DROP) ditulis sebagai komentar. Centang \"Sertakan perubahan destruktif\" atau hapus tanda komentarnya untuk menjalankan.", g.res.Skipped)
	}
	g.res.SQL = b.String()
}
