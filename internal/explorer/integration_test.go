package explorer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

const mixedTable = `Mixed Case "Q"`

type fixture struct {
	pool   *pgxpool.Pool
	schema string
	cat    *Catalog
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := testutil.RequireEnv(t, testutil.EnvTestDB)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	schema := fmt.Sprintf("exp_it_%d", time.Now().UnixNano())
	q := QuoteIdent(schema)
	stmts := []string{
		"CREATE SCHEMA " + q,
		`CREATE TABLE ` + q + `.customers (id serial PRIMARY KEY, name text NOT NULL, email text UNIQUE, note text DEFAULT 'none')`,
		`INSERT INTO ` + q + `.customers (name, email, note) SELECT 'cust ' || g, 'c' || g || '@x.test', CASE WHEN g % 10 = 0 THEN NULL ELSE '<b>' || g || '</b>' END FROM generate_series(1, 60) g`,
		`CREATE TABLE ` + q + `.orders (id serial PRIMARY KEY, customer_id int NOT NULL REFERENCES ` + q + `.customers(id), total numeric(10,2) CHECK (total >= 0))`,
		`CREATE INDEX orders_customer_idx ON ` + q + `.orders (customer_id)`,
		`INSERT INTO ` + q + `.orders (customer_id, total) VALUES (1, 10.50), (1, 0), (2, 99.99)`,
		`CREATE TABLE ` + q + `.nopk (a int, b text)`,
		`INSERT INTO ` + q + `.nopk VALUES (1, 'x'), (2, ''), (3, NULL)`,
		`CREATE TABLE ` + q + `.empty_t (id int PRIMARY KEY)`,
		`CREATE VIEW ` + q + `.order_totals AS SELECT customer_id, sum(total) AS s FROM ` + q + `.orders GROUP BY customer_id`,
		`CREATE TABLE ` + q + `.` + QuoteIdent(mixedTable) + ` (` + QuoteIdent(`Col "A"`) + ` int PRIMARY KEY)`,
		`INSERT INTO ` + q + `.` + QuoteIdent(mixedTable) + ` VALUES (1), (2)`,
	}
	t.Cleanup(func() { pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+q+" CASCADE") })
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("setup gagal pada %q: %v", s, err)
		}
	}
	cat, err := Introspect(ctx, pool)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	return &fixture{pool: pool, schema: schema, cat: cat}
}

func TestIntegrationIntrospect(t *testing.T) {
	f := setup(t)
	for _, s := range f.cat.Schemas {
		if s.Name == "information_schema" || strings.HasPrefix(s.Name, "pg_") {
			t.Errorf("schema sistem tidak boleh muncul: %s", s.Name)
		}
	}
	cust, ok := f.cat.Table(f.schema, "customers")
	if !ok {
		t.Fatal("customers tidak ditemukan")
	}
	if cust.Kind != KindTable || len(cust.Columns) != 4 {
		t.Fatalf("customers: %+v", cust)
	}
	if c := cust.Columns[0]; c.Name != "id" || c.Type != "integer" || c.Nullable || !c.HasDefault {
		t.Errorf("kolom id salah: %+v", c)
	}
	if c := cust.Columns[1]; c.Name != "name" || c.Nullable {
		t.Errorf("kolom name harus NOT NULL: %+v", c)
	}
	if c := cust.Columns[3]; !c.Nullable || !strings.Contains(c.Default, "none") {
		t.Errorf("default note salah: %+v", c)
	}
	var types []ConstraintType
	for _, c := range cust.Constraints {
		types = append(types, c.Type)
	}
	if len(types) != 2 || types[0] != PrimaryKey || types[1] != Unique {
		t.Errorf("constraint customers: %v", types)
	}
	if pk := cust.PrimaryKey(); len(pk) != 1 || pk[0] != "id" {
		t.Errorf("PrimaryKey = %v", pk)
	}
	if len(cust.Indexes) != 2 || !cust.Indexes[0].Primary {
		t.Errorf("index customers (PK dulu): %+v", cust.Indexes)
	}

	orders, _ := f.cat.Table(f.schema, "orders")
	got := map[ConstraintType]int{}
	for _, c := range orders.Constraints {
		got[c.Type]++
		if c.Type == ForeignKey && (len(c.Columns) != 1 || c.Columns[0] != "customer_id" || !strings.Contains(c.Definition, "REFERENCES")) {
			t.Errorf("FK salah: %+v", c)
		}
	}
	if got[PrimaryKey] != 1 || got[ForeignKey] != 1 || got[Check] != 1 {
		t.Errorf("constraint orders: %v", got)
	}
	foundIdx := false
	for _, ix := range orders.Indexes {
		if ix.Name == "orders_customer_idx" && !ix.Unique && !ix.Primary {
			foundIdx = true
		}
	}
	if !foundIdx {
		t.Errorf("orders_customer_idx tidak ditemukan: %+v", orders.Indexes)
	}
	if v, ok := f.cat.Table(f.schema, "order_totals"); !ok || v.Kind != KindView {
		t.Errorf("view tidak terdeteksi: %+v", v)
	}
}

func TestIntegrationFetchPagePagination(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	p1, err := FetchPage(ctx, f.pool, f.cat, f.schema, "customers", 1, 25)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Pagination.Total != 60 || p1.Pagination.TotalPages != 3 || len(p1.Rows) != 25 || p1.Ordering != OrderPK {
		t.Fatalf("halaman 1: %+v rows=%d", p1.Pagination, len(p1.Rows))
	}
	if p1.Rows[0][0].Text != "1" || p1.Rows[24][0].Text != "25" {
		t.Fatalf("urutan PK salah: %q..%q", p1.Rows[0][0].Text, p1.Rows[24][0].Text)
	}
	p3, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "customers", 3, 25)
	if len(p3.Rows) != 10 || p3.Rows[0][0].Text != "51" {
		t.Fatalf("halaman 3: rows=%d first=%v", len(p3.Rows), p3.Rows[0][0])
	}
	over, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "customers", 999, 25)
	if over.Pagination.Page != 3 || len(over.Rows) != 10 {
		t.Fatalf("melebihi batas harus ke halaman terakhir: %+v", over.Pagination)
	}
	zero, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "customers", 0, 25)
	if zero.Pagination.Page != 1 || zero.Rows[0][0].Text != "1" {
		t.Fatalf("halaman 0 harus jadi 1: %+v", zero.Pagination)
	}
	big, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "customers", 1, 100000)
	if big.Pagination.PageSize != MaxPageSize || len(big.Rows) != 60 {
		t.Fatalf("page size harus di-clamp: %+v rows=%d", big.Pagination, len(big.Rows))
	}
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		p, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "customers", page, 25)
		for _, r := range p.Rows {
			if seen[r[0].Text] {
				t.Fatalf("baris %s muncul dua kali", r[0].Text)
			}
			seen[r[0].Text] = true
		}
	}
	if len(seen) != 60 {
		t.Fatalf("hanya %d baris unik dari 60", len(seen))
	}
}

func TestIntegrationFetchPageCellsAndKinds(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	p, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "customers", 1, 25)
	// Baris ke-10 punya note NULL; baris 1 berisi HTML yang tidak boleh diubah di sisi Go.
	if !p.Rows[9][3].Null {
		t.Errorf("note baris 10 harus NULL: %+v", p.Rows[9][3])
	}
	if p.Rows[0][3].Text != "<b>1</b>" || p.Rows[0][3].Null {
		t.Errorf("isi HTML harus apa adanya: %+v", p.Rows[0][3])
	}

	np, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "nopk", 1, 25)
	if np.Ordering != OrderCTID || len(np.Rows) != 3 {
		t.Fatalf("nopk: ordering=%s rows=%d", np.Ordering, len(np.Rows))
	}
	if np.Rows[1][1].Null || np.Rows[1][1].Text != "" || !np.Rows[2][1].Null {
		t.Errorf("string kosong harus dibedakan dari NULL: %+v %+v", np.Rows[1][1], np.Rows[2][1])
	}

	v, err := FetchPage(ctx, f.pool, f.cat, f.schema, "order_totals", 1, 25)
	if err != nil || v.Ordering != OrderNone || v.Pagination.Total != 2 {
		t.Fatalf("view: %v %+v", err, v)
	}

	e, _ := FetchPage(ctx, f.pool, f.cat, f.schema, "empty_t", 1, 25)
	if e.Pagination.Total != 0 || e.Pagination.TotalPages != 1 || len(e.Rows) != 0 || len(e.Columns) != 1 {
		t.Fatalf("tabel kosong: %+v", e)
	}

	m, err := FetchPage(ctx, f.pool, f.cat, f.schema, mixedTable, 1, 25)
	if err != nil || len(m.Rows) != 2 || m.Columns[0] != `Col "A"` {
		t.Fatalf("nama dengan spasi dan kutip: %v %+v", err, m)
	}
}

func TestIntegrationLongCellTruncated(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	q := QuoteIdent(f.schema)
	if _, err := f.pool.Exec(ctx, "CREATE TABLE "+q+".big (id int PRIMARY KEY, body text)"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, "INSERT INTO "+q+".big VALUES (1, repeat('é', 100000))"); err != nil {
		t.Fatal(err)
	}
	cat, err := Introspect(ctx, f.pool)
	if err != nil {
		t.Fatal(err)
	}
	p, err := FetchPage(ctx, f.pool, cat, f.schema, "big", 1, 25)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Rows[0][1]
	if !c.Truncated || len([]rune(c.Text)) != MaxCellChars {
		t.Fatalf("sel panjang harus dipotong: truncated=%v len=%d", c.Truncated, len([]rune(c.Text)))
	}
}

func TestIntegrationWhitelistRejectsUnknown(t *testing.T) {
	f := setup(t)
	for _, c := range [][2]string{
		{f.schema, "nope"},
		{"nope", "customers"},
		{f.schema, "customers; DROP TABLE customers"},
		{f.schema, `"customers"`},
		{"pg_catalog", "pg_class"},
		{"information_schema", "tables"},
	} {
		if _, err := FetchPage(context.Background(), f.pool, f.cat, c[0], c[1], 1, 25); !errors.Is(err, ErrTableNotFound) {
			t.Errorf("%v harus ErrTableNotFound, dapat %v", c, err)
		}
	}
	// Tabel masih utuh setelah percobaan injeksi.
	if p, err := FetchPage(context.Background(), f.pool, f.cat, f.schema, "customers", 1, 1); err != nil || p.Pagination.Total != 60 {
		t.Fatalf("customers rusak: %v", err)
	}
}

type recordingBeginner struct {
	inner TxBeginner
	opts  pgx.TxOptions
	tx    pgx.Tx
}

func (r *recordingBeginner) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	r.opts = o
	tx, err := r.inner.BeginTx(ctx, o)
	r.tx = tx
	return tx, err
}

func TestIntegrationViewerIsReadOnlyAndRollsBack(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	rec := &recordingBeginner{inner: f.pool}
	if _, err := FetchPage(ctx, rec, f.cat, f.schema, "customers", 1, 5); err != nil {
		t.Fatal(err)
	}
	if rec.opts.AccessMode != pgx.ReadOnly {
		t.Fatalf("transaksi harus read-only, dapat %q", rec.opts.AccessMode)
	}
	// Setelah FetchPage, transaksi harus rollback
	if _, err := rec.tx.Exec(ctx, "SELECT 1"); !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("tx harus sudah ditutup, dapat %v", err)
	}
	tx, err := f.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, "DELETE FROM "+QuoteQualified(f.schema, "customers"))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("tulis di tx read-only harus ditolak 25006, dapat %v", err)
	}
}

func TestIntegrationUnreachableTargetIsConnectionError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	pool, err := pgxpool.New(context.Background(), fmt.Sprintf("postgres://u:supersecret@127.0.0.1:%d/x?sslmode=disable&connect_timeout=2", port))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	_, err = Introspect(context.Background(), pool)
	if !errors.Is(err, dbconn.ErrConnectionFailed) {
		t.Fatalf("Introspect pada target mati harus ErrConnectionFailed, dapat %v", err)
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatal("error membocorkan password")
	}

	cat := &Catalog{Schemas: []*Schema{{Name: "public", Tables: []*Table{{Schema: "public", Name: "t", Kind: KindTable, Columns: []Column{{Name: "a"}}}}}}}
	_, err = FetchPage(context.Background(), pool, cat, "public", "t", 1, 25)
	if !errors.Is(err, dbconn.ErrConnectionFailed) {
		t.Fatalf("FetchPage pada target mati harus ErrConnectionFailed, dapat %v", err)
	}
}
