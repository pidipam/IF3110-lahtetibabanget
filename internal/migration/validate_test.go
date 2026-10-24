package migration

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateTitle(t *testing.T) {
	if _, err := ValidateTitle("   "); !errors.Is(err, ErrEmptyTitle) {
		t.Errorf("judul kosong: %v", err)
	}
	if _, err := ValidateTitle(strings.Repeat("a", 101)); !errors.Is(err, ErrTitleTooLong) {
		t.Errorf("judul panjang: %v", err)
	}
	if got, err := ValidateTitle("  tambah tabel  "); err != nil || got != "tambah tabel" {
		t.Errorf("judul valid: %q %v", got, err)
	}
}

func TestValidateSQL(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want error
	}{
		{"kosong", "   ", ErrEmptySQL},
		{"hanya komentar", "-- apa saja\n/* blok */;", ErrEmptySQL},
		{"valid", "CREATE TABLE t (id int);", nil},
		{"multi statement", "CREATE TABLE a (id int); INSERT INTO a VALUES (1);", nil},
		{"commit", "CREATE TABLE a (id int); COMMIT;", ErrTxControl},
		{"begin huruf kecil", "begin; drop table a;", ErrTxControl},
		{"rollback", "INSERT INTO a VALUES (1);\nROLLBACK", ErrTxControl},
		{"savepoint", "SAVEPOINT x;", ErrTxControl},
		{"start transaction", "START   TRANSACTION;", ErrTxControl},
		{"kata commit di string", "INSERT INTO log VALUES ('; commit');", nil},
		{"kata commit di komentar", "-- commit\nCREATE TABLE t (id int);", nil},
		{"fungsi plpgsql", "CREATE FUNCTION f() RETURNS void AS $$ BEGIN PERFORM 1; END; $$ LANGUAGE plpgsql;", nil},
		{"fungsi dengan tag", "CREATE FUNCTION f() RETURNS void AS $body$ BEGIN NULL; END; $body$ LANGUAGE plpgsql;", nil},
		{"null byte", "SELECT 1;\x00", ErrInvalidSQL},
		{"terlalu panjang", strings.Repeat("a", MaxSQLBytes+1), ErrSQLTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ValidateSQL(c.sql)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}
