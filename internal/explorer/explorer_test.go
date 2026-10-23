package explorer

import (
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func TestQuoteIdent(t *testing.T) {
	tests := []struct{ in, want string }{
		{"users", `"users"`},
		{"Mixed Case", `"Mixed Case"`},
		{`we"ird`, `"we""ird"`},
		{`"; DROP TABLE users; --`, `"""; DROP TABLE users; --"`},
		{"nul\x00byte", `"nulbyte"`},
		{"", `""`},
	}
	for _, tc := range tests {
		if got := QuoteIdent(tc.in); got != tc.want {
			t.Errorf("QuoteIdent(%q) = %s, mau %s", tc.in, got, tc.want)
		}
	}
	if got := QuoteQualified("public", `a"b`); got != `"public"."a""b"` {
		t.Errorf("QuoteQualified = %s", got)
	}
}

func TestParsePageParams(t *testing.T) {
	tests := []struct {
		page, size         string
		wantPage, wantSize int
	}{
		{"", "", 1, 25},
		{"3", "10", 3, 10},
		{"0", "0", 1, 25},
		{"-5", "-1", 1, 25},
		{"abc", "xyz", 1, 25},
		{"2", "1", 2, 1},
		{"2", "100", 2, 100},
		{"2", "101", 2, 100},
		{"2", "100000", 2, 100},
		{" 4 ", " 50 ", 4, 50},
		{"99999999999999999999", "99999999999999999999", 1, 25},
		{"1.5", "2.5", 1, 25},
	}
	for _, tc := range tests {
		p, s := ParsePageParams(tc.page, tc.size)
		if p != tc.wantPage || s != tc.wantSize {
			t.Errorf("ParsePageParams(%q,%q) = %d,%d; mau %d,%d", tc.page, tc.size, p, s, tc.wantPage, tc.wantSize)
		}
	}
}

func TestPaginate(t *testing.T) {
	tests := []struct {
		name                         string
		total                        int64
		page, size                   int
		wantPage, wantPages          int
		wantOffset, wantFrom, wantTo int64
		prev, next                   bool
	}{
		{"kosong", 0, 1, 25, 1, 1, 0, 0, 0, false, false},
		{"satu baris", 1, 1, 25, 1, 1, 0, 1, 1, false, false},
		{"pas satu halaman", 25, 1, 25, 1, 1, 0, 1, 25, false, false},
		{"26 baris halaman 1", 26, 1, 25, 1, 2, 0, 1, 25, false, true},
		{"26 baris halaman 2", 26, 2, 25, 2, 2, 25, 26, 26, true, false},
		{"200 baris halaman 5 size 25", 200, 5, 25, 5, 8, 100, 101, 125, true, true},
		{"halaman 0 jadi 1", 200, 0, 25, 1, 8, 0, 1, 25, false, true},
		{"halaman negatif jadi 1", 200, -3, 25, 1, 8, 0, 1, 25, false, true},
		{"melebihi batas jadi terakhir", 200, 999, 25, 8, 8, 175, 176, 200, true, false},
		{"size berlebihan di-clamp", 500, 1, 100000, 1, 5, 0, 1, 100, false, true},
		{"size nol jadi default", 100, 1, 0, 1, 4, 0, 1, 25, false, true},
		{"total negatif dianggap kosong", -4, 1, 25, 1, 1, 0, 0, 0, false, false},
		{"tabel kosong, halaman jauh", 0, 50, 25, 1, 1, 0, 0, 0, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Paginate(tc.total, tc.page, tc.size)
			if p.Page != tc.wantPage || p.TotalPages != tc.wantPages || p.Offset != tc.wantOffset || p.From != tc.wantFrom || p.To != tc.wantTo || p.HasPrev != tc.prev || p.HasNext != tc.next {
				t.Fatalf("hasil %+v", p)
			}
			if p.HasPrev && p.PrevPage != p.Page-1 || p.HasNext && p.NextPage != p.Page+1 {
				t.Fatalf("prev/next salah: %+v", p)
			}
		})
	}
}

func TestPaginateHugeTotalNoOverflow(t *testing.T) {
	p := Paginate(1<<62, 1<<30, 100)
	if p.Offset < 0 || p.Page < 1 {
		t.Fatalf("overflow: %+v", p)
	}
}

func TestNewCell(t *testing.T) {
	if c := NewCell(nil); !c.Null || c.Text != "" {
		t.Fatalf("nil harus Null: %+v", c)
	}
	if c := NewCell(ptr("")); c.Null || c.Text != "" {
		t.Fatalf("string kosong bukan NULL: %+v", c)
	}
	if c := NewCell(ptr("<script>x</script>")); c.Text != "<script>x</script>" {
		t.Fatalf("NewCell tidak boleh mengubah isi (escape dilakukan template): %+v", c)
	}
	exact := strings.Repeat("a", MaxCellChars)
	if c := NewCell(&exact); c.Truncated || c.Text != exact {
		t.Fatal("tepat batas tidak boleh dipotong")
	}
	long := strings.Repeat("é", MaxCellChars+1)
	c := NewCell(&long)
	if !c.Truncated || len([]rune(c.Text)) != MaxCellChars {
		t.Fatalf("harus dipotong per karakter, bukan byte: %d", len([]rune(c.Text)))
	}
}

func sampleCatalog() *Catalog {
	return assemble(
		[]string{"public", "empty", "audit"},
		[]rawTable{
			{"public", "orders", "r"}, {"public", "users", "r"}, {"public", "user_names", "v"},
			{"audit", "log", "r"}, {"public", "stats", "m"}, {"public", "part", "p"},
		},
		[]rawColumn{
			{"public", "users", "email", "text", 2, false, nil},
			{"public", "users", "id", "bigint", 1, false, ptr("nextval('users_id_seq'::regclass)")},
			{"public", "orders", "id", "bigint", 1, false, nil},
			{"public", "orders", "user_id", "bigint", 2, true, nil},
			{"ghost", "nope", "x", "int", 1, true, nil},
		},
		[]rawConstraint{
			{"public", "orders", "orders_user_fk", "f", "FOREIGN KEY (user_id) REFERENCES users(id)", []string{"user_id"}},
			{"public", "orders", "orders_pkey", "p", "PRIMARY KEY (id)", []string{"id"}},
			{"public", "users", "users_email_key", "u", "UNIQUE (email)", []string{"email"}},
			{"public", "users", "users_email_chk", "c", "CHECK (email <> '')", []string{"email"}},
			{"public", "users", "users_pkey", "p", "PRIMARY KEY (id)", []string{"id"}},
			{"public", "users", "x_notnull", "n", "NOT NULL id", nil},
		},
		[]rawIndex{
			{"public", "users", "users_email_key", "CREATE UNIQUE INDEX ...", true, false},
			{"public", "users", "users_pkey", "CREATE UNIQUE INDEX ...", true, true},
			{"public", "users", "a_idx", "CREATE INDEX ...", false, false},
		},
	)
}

func TestAssembleStructure(t *testing.T) {
	cat := sampleCatalog()
	var names []string
	for _, s := range cat.Schemas {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "audit,empty,public" {
		t.Fatalf("schema harus terurut dan memuat schema kosong: %v", names)
	}
	if s, _ := cat.Schema("empty"); len(s.Tables) != 0 {
		t.Fatal("schema empty harus tanpa tabel")
	}
	if _, ok := cat.Schema("ghost"); ok {
		t.Fatal("kolom dengan tabel induk tak dikenal tidak boleh membuat schema baru")
	}

	users, ok := cat.Table("public", "users")
	if !ok {
		t.Fatal("users tidak ditemukan")
	}
	if users.Columns[0].Name != "id" || users.Columns[1].Name != "email" {
		t.Fatalf("kolom harus urut posisi: %+v", users.Columns)
	}
	if !users.Columns[0].HasDefault || users.Columns[0].Default == "" || users.Columns[1].HasDefault {
		t.Fatalf("default salah: %+v", users.Columns)
	}
	if users.Columns[0].Nullable || users.Columns[1].Nullable {
		t.Fatal("nullable salah")
	}
	var order []string
	for _, c := range users.Constraints {
		order = append(order, c.Name)
	}
	if strings.Join(order, ",") != "users_pkey,users_email_key,users_email_chk" {
		t.Fatalf("urutan constraint (PK, UNIQUE, FK, CHECK): %v", order)
	}
	if users.Indexes[0].Name != "users_pkey" || users.Indexes[1].Name != "a_idx" {
		t.Fatalf("PK index harus pertama: %+v", users.Indexes)
	}
	if pk := users.PrimaryKey(); len(pk) != 1 || pk[0] != "id" {
		t.Fatalf("PrimaryKey = %v", pk)
	}
	orders, _ := cat.Table("public", "orders")
	if orders.Constraints[0].Type != PrimaryKey || orders.Constraints[1].Type != ForeignKey {
		t.Fatalf("PK harus sebelum FK: %+v", orders.Constraints)
	}
	if v, _ := cat.Table("public", "user_names"); v.Kind != KindView || v.Kind.Label() != "View" {
		t.Fatalf("kind view salah: %+v", v)
	}
}

func TestCatalogWhitelist(t *testing.T) {
	cat := sampleCatalog()
	for _, bad := range [][2]string{{"public", "nope"}, {"nope", "users"}, {"public", "users; DROP TABLE users"}, {"public", `"users"`}, {"", ""}, {"PUBLIC", "users"}} {
		if _, ok := cat.Table(bad[0], bad[1]); ok {
			t.Errorf("%v seharusnya tidak lolos whitelist", bad)
		}
	}
}

func TestChooseOrdering(t *testing.T) {
	cat := sampleCatalog()
	want := map[string]Ordering{"users": OrderPK, "orders": OrderPK, "user_names": OrderNone, "stats": OrderCTID, "part": OrderNone}
	for name, w := range want {
		tbl, _ := cat.Table("public", name)
		if got := ChooseOrdering(tbl); got != w {
			t.Errorf("%s: %s, mau %s", name, got, w)
		}
	}
	log, _ := cat.Table("audit", "log")
	if ChooseOrdering(log) != OrderCTID {
		t.Error("tabel tanpa PK harus pakai ctid")
	}
}

func TestBuildSelect(t *testing.T) {
	tbl := &Table{Schema: "public", Name: "users", Kind: KindTable,
		Columns:     []Column{{Name: "id"}, {Name: `we"ird`}},
		Constraints: []Constraint{{Type: PrimaryKey, Columns: []string{"id"}}}}
	got, ok := BuildSelect(tbl, OrderPK)
	want := `SELECT left(t."id"::text, 501) AS "id", left(t."we""ird"::text, 501) AS "we""ird" FROM "public"."users" AS t ORDER BY t."id" LIMIT $1 OFFSET $2`
	if !ok || got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}

	got, _ = BuildSelect(tbl, OrderCTID)
	if !strings.Contains(got, "ORDER BY t.ctid LIMIT $1 OFFSET $2") {
		t.Fatalf("ctid: %s", got)
	}
	got, _ = BuildSelect(tbl, OrderNone)
	if strings.Contains(got, "ORDER BY") {
		t.Fatalf("OrderNone tidak boleh punya ORDER BY: %s", got)
	}

	tbl.Constraints = []Constraint{{Type: PrimaryKey, Columns: []string{"a", "b"}}}
	got, _ = BuildSelect(tbl, OrderPK)
	if !strings.Contains(got, `ORDER BY t."a", t."b" LIMIT`) {
		t.Fatalf("PK komposit: %s", got)
	}

	if _, ok := BuildSelect(&Table{Schema: "s", Name: "t"}, OrderNone); ok {
		t.Fatal("tabel tanpa kolom harus ok=false")
	}
}

func TestBuildSelectInjectionNames(t *testing.T) {
	evil := `x"; DROP TABLE users; --`
	tbl := &Table{Schema: evil, Name: evil, Kind: KindTable, Columns: []Column{{Name: evil}}}
	sql, _ := BuildSelect(tbl, OrderCTID)
	// Setelah quoting, setiap tanda kutip dari input tergandakan sehingga tidak bisa menutup identifier.
	if strings.Count(sql, `x""; DROP TABLE users; --`) != 4 {
		t.Fatalf("identifier tidak ter-escape: %s", sql)
	}
	count := BuildCount(tbl)
	if count != `SELECT count(*) FROM "x""; DROP TABLE users; --"."x""; DROP TABLE users; --"` {
		t.Fatalf("count: %s", count)
	}
}
