package migration

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{{"satu tanpa titik koma", "SELECT 1", []string{"SELECT 1"}}, {"dua statement", "SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}}, {"titik koma di string", "INSERT INTO t VALUES ('a;b'); SELECT 2", []string{"INSERT INTO t VALUES ('a;b')", "SELECT 2"}}, {"titik koma di identifier", `SELECT 1 AS "x;y"; SELECT 2`, []string{`SELECT 1 AS "x;y"`, "SELECT 2"}}, {"komentar", "-- catatan; bukan pemisah\nSELECT 1; /* ; */ SELECT 2", []string{"-- catatan; bukan pemisah\nSELECT 1", "/* ; */ SELECT 2"}}, {"dollar quote", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql; SELECT 2", []string{"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql", "SELECT 2"}}, {"hanya komentar dan titik koma", "; -- kosong\n;", nil}, {"titik koma di backtick", "SELECT 1 AS `x;y`; SELECT 2", []string{"SELECT 1 AS `x;y`", "SELECT 2"}}}
	for _, c := range cases {
		if got := SplitStatements(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestExplainable(t *testing.T) {
	yes := []string{"SELECT 1", "  select * from t", "(SELECT 1)", "WITH x AS (SELECT 1) SELECT * FROM x", "INSERT INTO t VALUES (1)", "UPDATE t SET a = 1", "DELETE FROM t", "-- catatan\nSELECT 1", "TABLE customers"}
	no := []string{"CREATE TABLE t (id int)", "ALTER TABLE t ADD COLUMN x int", "CREATE INDEX i ON t (a)", "DROP TABLE t", "TRUNCATE t", "COMMENT ON TABLE t IS 'select'"}
	for _, s := range yes {
		if !Explainable(s) {
			t.Errorf("%q seharusnya bisa di-EXPLAIN", s)
		}
	}
	for _, s := range no {
		if Explainable(s) {
			t.Errorf("%q seharusnya tidak bisa di-EXPLAIN", s)
		}
	}
}

func TestParsePlanFlattensTree(t *testing.T) {
	raw := `[{"Plan": {"Node Type": "Hash Join", "Join Type": "Inner", "Startup Cost": 1.5, "Total Cost": 10.25, "Plan Rows": 7, "Actual Rows": 6, "Actual Total Time": 0.42, "Actual Loops": 1, "Plans": [{"Node Type": "Seq Scan", "Relation Name": "orders", "Startup Cost": 0, "Total Cost": 4, "Plan Rows": 100}, {"Node Type": "Hash", "Startup Cost": 1, "Total Cost": 1, "Plan Rows": 3, "Plans": [{"Node Type": "Index Scan", "Relation Name": "customers", "Index Name": "customers_pkey", "Startup Cost": 0.15, "Total Cost": 1, "Plan Rows": 3}]}]}, "Planning Time": 0.1, "Execution Time": 0.5}]`
	nodes, planning, execution, err := ParsePlan([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 4 {
		t.Fatalf("jumlah node %d, want 4", len(nodes))
	}
	root := nodes[0]
	if root.Node != "Hash Join" || root.Join != "Inner" || root.Depth != 0 || !root.HasActual || root.ActualRows != 6 || root.TotalCost != 10.25 {
		t.Fatalf("node akar salah: %+v", root)
	}
	if nodes[1].Relation != "orders" || nodes[1].Depth != 1 || nodes[1].HasActual {
		t.Fatalf("node seq scan salah: %+v", nodes[1])
	}
	if nodes[3].Index != "customers_pkey" || nodes[3].Depth != 2 {
		t.Fatalf("node index scan salah: %+v", nodes[3])
	}
	if nodes[0].Prefix != "" || nodes[1].Prefix != "├─ " || nodes[2].Prefix != "└─ " || nodes[3].Prefix != "   └─ " {
		t.Fatalf("garis pohon salah: %q %q %q %q", nodes[0].Prefix, nodes[1].Prefix, nodes[2].Prefix, nodes[3].Prefix)
	}
	if planning == nil || *planning != 0.1 || execution == nil || *execution != 0.5 {
		t.Fatal("waktu perencanaan atau eksekusi salah")
	}
}

func TestParsePlanModifyTable(t *testing.T) {
	nodes, _, _, err := ParsePlan([]byte(`[{"Plan": {"Node Type": "ModifyTable", "Operation": "Update", "Relation Name": "customers"}}]`))
	if err != nil || nodes[0].Node != "ModifyTable (Update)" {
		t.Fatalf("operasi ModifyTable tidak tampil: %+v %v", nodes, err)
	}
	if _, _, _, err := ParsePlan([]byte("bukan json")); err == nil {
		t.Fatal("JSON rusak seharusnya error")
	}
}

func TestExplainPlanDoesNotPersist(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	sql := "CREATE TABLE zz_mig_t1 (id int); INSERT INTO zz_mig_t1 SELECT generate_series(1, 100); SELECT * FROM zz_mig_t1 WHERE id = 5;"
	ex, err := x.e.Explain(ctx, 1, sql, false)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Analyze || ex.Stopped || len(ex.Statements) != 3 {
		t.Fatalf("hasil tidak lengkap: %+v", ex)
	}
	if !ex.Statements[0].NoPlan || ex.Statements[0].Number != 1 {
		t.Fatal("CREATE TABLE seharusnya ditandai tanpa plan")
	}
	last := ex.Statements[2]
	if len(last.Nodes) == 0 || last.Nodes[0].Relation != "zz_mig_t1" || last.Nodes[0].HasActual {
		t.Fatalf("plan SELECT salah: %+v", last.Nodes)
	}
	if x.tableExists(t, "zz_mig_t1") {
		t.Fatal("EXPLAIN tidak boleh meninggalkan perubahan di target")
	}
}

func TestExplainAnalyzeMeasuresAndRollsBack(t *testing.T) {
	x := setupExec(t)
	ctx := context.Background()
	sql := "CREATE TABLE zz_mig_t1 (id int); INSERT INTO zz_mig_t1 SELECT generate_series(1, 100); SELECT * FROM zz_mig_t1 WHERE id > 90;"
	ex, err := x.e.Explain(ctx, 1, sql, true)
	if err != nil {
		t.Fatal(err)
	}
	last := ex.Statements[2]
	if !ex.Analyze || len(last.Nodes) == 0 || !last.Nodes[0].HasActual || last.Nodes[0].ActualRows != 10 || !last.HasTiming {
		t.Fatalf("EXPLAIN ANALYZE tidak mengukur baris aktual: %+v", last)
	}
	if x.tableExists(t, "zz_mig_t1") {
		t.Fatal("EXPLAIN ANALYZE tidak boleh meninggalkan perubahan di target")
	}
}

func TestExplainStopsAtFailedStatement(t *testing.T) {
	x := setupExec(t)
	ex, err := x.e.Explain(context.Background(), 1, "SELECT * FROM zz_tidak_ada; SELECT 1;", false)
	if err != nil {
		t.Fatal(err)
	}
	if !ex.Stopped || len(ex.Statements) != 1 || !strings.Contains(ex.Statements[0].Error, "zz_tidak_ada") {
		t.Fatalf("analisis seharusnya berhenti di statement yang gagal: %+v", ex)
	}
}

func TestExplainUnknownTarget(t *testing.T) {
	x := setupExec(t)
	if _, err := x.e.Explain(context.Background(), 99, "SELECT 1", false); err != ErrTargetNotFound {
		t.Fatalf("want ErrTargetNotFound, got %v", err)
	}
}
