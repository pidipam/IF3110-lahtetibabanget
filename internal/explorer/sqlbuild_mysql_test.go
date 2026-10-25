package explorer

import (
	"strings"
	"testing"
)

func TestQuoteIdentMySQL(t *testing.T) {
	if got := QuoteIdentMySQL("a`b"); got != "`a``b`" {
		t.Fatalf("got %s", got)
	}
	if got := QuoteIdentMySQL("x\x00y"); got != "`xy`" {
		t.Fatalf("got %s", got)
	}
}

func TestBuildSelectMySQL(t *testing.T) {
	tb := &Table{Schema: "s", Name: "t", Kind: KindTable,
		Columns:     []Column{{Name: "id"}, {Name: "n`ame"}},
		Constraints: []Constraint{{Type: PrimaryKey, Columns: []string{"id"}}}}
	q, ok := BuildSelectMySQL(tb, ChooseOrderingMySQL(tb))
	if !ok {
		t.Fatal("harus ok")
	}
	for _, want := range []string{"LIMIT ? OFFSET ?", "ORDER BY t.`id`", "`n``ame`"} {
		if !strings.Contains(q, want) {
			t.Fatalf("query tidak memuat %q: %s", want, q)
		}
	}
	if strings.Contains(q, "ctid") || strings.Contains(q, "$1") {
		t.Fatalf("query masih memakai sintaks PG: %s", q)
	}
}

func TestChooseOrderingMySQLNoPK(t *testing.T) {
	if got := ChooseOrderingMySQL(&Table{Kind: KindTable}); got != OrderNone {
		t.Fatalf("tanpa PK harus OrderNone, dapat %v", got)
	}
}
