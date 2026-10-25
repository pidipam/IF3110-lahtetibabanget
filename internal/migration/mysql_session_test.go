package migration

import (
	"context"
	"database/sql"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func openMariaDB(t *testing.T) *sql.DB {
	t.Helper()
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
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS zz_mig_my1, zz_mig_my2")
		db.Close()
	})
	db.Exec("DROP TABLE IF EXISTS zz_mig_my1, zz_mig_my2")
	return db
}

func runMySQL(t *testing.T, db *sql.DB, q string) error {
	t.Helper()
	ctx := context.Background()
	s, err := newMySQLSession(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	return s.Run(ctx, q, 10*time.Second)
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMySQLSessionSuccessCommits(t *testing.T) {
	db := openMariaDB(t)
	if err := runMySQL(t, db, "CREATE TABLE zz_mig_my1 (id INT); INSERT INTO zz_mig_my1 VALUES (1);"); err != nil {
		t.Fatalf("migration valid harus sukses: %v", err)
	}
	if countRows(t, db, "zz_mig_my1") != 1 {
		t.Fatal("hasil migration harus tersimpan")
	}
}

func TestMySQLSessionDMLRollsBackOnFailure(t *testing.T) {
	db := openMariaDB(t)
	if err := runMySQL(t, db, "CREATE TABLE zz_mig_my1 (id INT); INSERT INTO zz_mig_my1 VALUES (1);"); err != nil {
		t.Fatal(err)
	}
	err := runMySQL(t, db, "INSERT INTO zz_mig_my1 VALUES (2); SELECT * FROM zz_tidak_ada;")
	if err == nil || !strings.Contains(err.Error(), "statement 2") {
		t.Fatalf("harus gagal di statement 2, dapat %v", err)
	}
	if n := countRows(t, db, "zz_mig_my1"); n != 1 {
		t.Fatalf("DML harus di-rollback, baris = %d, want 1", n)
	}
}

func TestMySQLSessionPartialDDLIsReported(t *testing.T) {
	db := openMariaDB(t)
	err := runMySQL(t, db, "CREATE TABLE zz_mig_my2 (id INT); SELECT * FROM zz_tidak_ada;")
	if err == nil || !strings.Contains(err.Error(), "tidak dapat di-rollback") {
		t.Fatalf("pesan harus memperingatkan DDL parsial, dapat %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'zz_mig_my2'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("DDL sebelum kegagalan tetap tertinggal (perilaku MySQL): n=%d err=%v", n, err)
	}
}
