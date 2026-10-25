package explorer_test

import (
	"context"
	"database/sql"
	"net"
	"strconv"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/explorer"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func TestMariaDBIntrospectAndViewer(t *testing.T) {
	addr := testutil.RequireEnv(t, testutil.EnvTargetMariaDB)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	p := dbconn.Params{Engine: dbconn.EngineMariaDB, Host: host, Port: port, DBName: "sample", Username: "app", Password: "app", SSLMode: "disable"}
	db, err := sql.Open("mysql", p.MySQLDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	cat, err := explorer.IntrospectSQL(ctx, db)
	if err != nil {
		t.Fatalf("IntrospectSQL: %v", err)
	}

	cust, ok := cat.Table("sample", "customers")
	if !ok {
		t.Fatal("tabel customers tidak ditemukan")
	}
	if pk := cust.PrimaryKey(); len(pk) != 1 || pk[0] != "id" {
		t.Fatalf("primary key salah: %v", pk)
	}
	hasUnique, hasIdx := false, false
	for _, c := range cust.Constraints {
		if c.Type == explorer.Unique {
			hasUnique = true
		}
	}
	for _, i := range cust.Indexes {
		if i.Name == "customers_city_idx" {
			hasIdx = true
		}
	}
	if !hasUnique || !hasIdx {
		t.Fatalf("unique=%v index=%v, keduanya harus ada", hasUnique, hasIdx)
	}

	orders, _ := cat.Table("sample", "orders")
	hasFK := false
	for _, c := range orders.Constraints {
		if c.Type == explorer.ForeignKey {
			hasFK = true
		}
	}
	if !hasFK {
		t.Fatal("foreign key orders tidak terbaca")
	}
	if v, ok := cat.Table("sample", "order_totals"); !ok || v.Kind != explorer.KindView {
		t.Fatalf("order_totals harus terbaca sebagai view: %+v", v)
	}
	legacy, ok := cat.Table("sample", "legacy_notes")
	if !ok || len(legacy.PrimaryKey()) != 0 {
		t.Fatal("legacy_notes harus ada dan tanpa primary key")
	}

	dp, err := explorer.FetchPageSQL(ctx, db, cat, "sample", "customers", 1, 25)
	if err != nil {
		t.Fatalf("FetchPageSQL: %v", err)
	}
	if dp.Ordering != explorer.OrderPK || len(dp.Rows) != 3 {
		t.Fatalf("ordering=%v baris=%d", dp.Ordering, len(dp.Rows))
	}
	if !dp.Rows[1][5].Null { // customer 002: note NULL
		t.Fatal("sel NULL harus terbaca sebagai Null")
	}

	lp, err := explorer.FetchPageSQL(ctx, db, cat, "sample", "legacy_notes", 1, 25)
	if err != nil || lp.Ordering != explorer.OrderNone || len(lp.Rows) != 2 {
		t.Fatalf("legacy_notes: err=%v ordering=%v", err, lp)
	}
	if _, err := explorer.FetchPageSQL(ctx, db, cat, "sample", "tidak_ada", 1, 25); err != explorer.ErrTableNotFound {
		t.Fatalf("tabel tak dikenal harus ErrTableNotFound, dapat %v", err)
	}
}
