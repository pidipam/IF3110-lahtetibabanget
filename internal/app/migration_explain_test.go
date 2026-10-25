package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func TestMigrationExplainOnReviewPage(t *testing.T) {
	f := setupMigPages(t)
	e := f.e
	ctx := context.Background()
	conn := testutil.SeedConnection(t, e.db)
	migGrant(e, e.userGroup("editor"), conn.ID, "edit")
	migGrant(e, e.userGroup("viewer"), conn.ID, "view")
	base := fmt.Sprintf("/databases/%d/migrations", conn.ID)
	target, err := pgxpool.New(ctx, testutil.RequireEnv(t, "TARGET_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.Close)
	changed := func() int {
		var n int
		if err := target.QueryRow(ctx, "SELECT count(*) FROM customers WHERE name = 'zz-diubah-explain'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	ed := e.client()
	expectStatus(t, ed.login("editor", "password123"), http.StatusSeeOther)
	sql := "UPDATE customers SET name = 'zz-diubah-explain'; SELECT c.name FROM customers c JOIN orders o ON o.customer_id = c.id;"
	w := ed.post(base, url.Values{"title": {"analisis"}, "sql": {sql}})
	expectStatus(t, w, http.StatusSeeOther)
	path := strings.SplitN(w.Header().Get("Location"), "?", 2)[0]

	review := ed.do("GET", path+"/review", nil)
	expectStatus(t, review, http.StatusOK)
	if strings.Contains(review.Body.String(), "belum didukung") {
		t.Fatal("halaman review PostgreSQL tidak boleh menampilkan pesan 'belum didukung'")
	}
	if !strings.Contains(review.Body.String(), "EXPLAIN ANALYZE") {
		t.Fatal("halaman review tidak menampilkan tombol EXPLAIN")
	}

	plan := ed.post(path+"/explain", url.Values{"mode": {"plan"}})
	expectStatus(t, plan, http.StatusOK)
	body := plan.Body.String()
	for _, want := range []string{"Hasil EXPLAIN", "ModifyTable (Update)", "customers", "Estimasi cost", "Jalankan migration"} {
		if !strings.Contains(body, want) {
			t.Fatalf("hasil EXPLAIN tidak memuat %q", want)
		}
	}
	if !strings.Contains(body, "Join") || strings.Contains(body, "Baris aktual") {
		t.Fatal("EXPLAIN biasa harus menampilkan join tanpa kolom aktual")
	}

	an := ed.post(path+"/explain", url.Values{"mode": {"analyze"}})
	expectStatus(t, an, http.StatusOK)
	if !strings.Contains(an.Body.String(), "Hasil EXPLAIN ANALYZE") || !strings.Contains(an.Body.String(), "Baris aktual") || !strings.Contains(an.Body.String(), "waktu eksekusi") {
		t.Fatal("hasil EXPLAIN ANALYZE tidak menampilkan data aktual")
	}
	if changed() != 0 {
		t.Fatal("EXPLAIN ANALYZE mengubah data di database target")
	}
	if e.count("SELECT count(*) FROM migrations WHERE db_id = $1 AND status = 'draft'", conn.ID) != 1 {
		t.Fatal("EXPLAIN tidak boleh mengubah status migration")
	}

	expectStatus(t, ed.do("POST", path+"/explain", url.Values{"mode": {"plan"}}), http.StatusForbidden)
	v := e.client()
	expectStatus(t, v.login("viewer", "password123"), http.StatusSeeOther)
	expectStatus(t, v.post(path+"/explain", url.Values{"mode": {"analyze"}}), http.StatusForbidden)
	if changed() != 0 {
		t.Fatal("View Only berhasil menjalankan EXPLAIN ANALYZE")
	}
}
