package explorer

import "testing"

func TestWithDefDropsObjectsDroppedDuringIntrospection(t *testing.T) {
	in := []rawIndex{{Name: "a", Def: "CREATE INDEX a ON t (x)"}, {Name: "gone"}, {Name: "b", Def: "CREATE INDEX b ON t (y)"}}
	out := withDef(in, func(i rawIndex) string { return i.Def })
	if len(out) != 2 || out[0].Name != "a" || out[1].Name != "b" {
		t.Fatalf("index tanpa definisi harus dilewati: %+v", out)
	}
	if got := withDef([]rawConstraint{}, func(c rawConstraint) string { return c.Def }); len(got) != 0 {
		t.Fatalf("kosong: %+v", got)
	}
}
