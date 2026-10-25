package schemadiff

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestQuoteIdent(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"users", "users"},
		{"user_id", "user_id"},
		{"col$1", "col$1"},
		{"User", `"User"`},
		{"Sales Data", `"Sales Data"`},
		{"order", `"order"`},
		{"select", `"select"`},
		{`a"b`, `"a""b"`},
		{"1st", `"1st"`},
		{"naïve", `"naïve"`},
	} {
		if got := QuoteIdent(tc.in); got != tc.want {
			t.Fatalf("QuoteIdent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidSchemaName(t *testing.T) {
	for _, name := range []string{"public", "analytics", "Sales Data", "a_b"} {
		if !validSchemaName(name) {
			t.Fatalf("%q harus valid", name)
		}
	}
	for _, name := range []string{"", "pg_catalog", "pg_toast", "information_schema", strings.Repeat("a", 64)} {
		if validSchemaName(name) {
			t.Fatalf("%q harus ditolak", name)
		}
	}
}

func TestNewScratchSchema(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		name, err := newScratchSchema()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(name, scratchPrefix) || len(name) != len(scratchPrefix)+16 {
			t.Fatalf("nama schema sementara tidak sesuai: %q", name)
		}
		if !validSchemaName(name) {
			t.Fatalf("nama schema sementara tidak valid: %q", name)
		}
		if seen[name] {
			t.Fatalf("nama schema sementara berulang: %q", name)
		}
		seen[name] = true
	}
}

func TestASDDLError(t *testing.T) {
	raw := &pgconn.PgError{Message: "syntax error at or near \"x\"", Hint: "periksa DDL"}
	err := asDDLError(raw)
	var de *DDLError
	if !errors.As(err, &de) {
		t.Fatalf("error harus DDLError: %T", err)
	}
	if !strings.Contains(de.Error(), "syntax error") || !strings.Contains(de.Error(), "periksa DDL") {
		t.Fatalf("message tidak lengkap: %q", de.Error())
	}
	if !errors.Is(err, raw) {
		t.Fatal("DDLError harus membungkus error asli")
	}
	mustNotContain(t, de.Error(), "postgres://", "password")

	generic := asDDLError(errors.New("boom"))
	if !errors.As(generic, &de) || !strings.Contains(de.Error(), "gagal dijalankan") {
		t.Fatalf("error generik salah: %v", generic)
	}
}

func TestTruncateMessage(t *testing.T) {
	if got := truncateMessage("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncateMessage("abcdef", 3); got != "abc..." {
		t.Fatalf("got %q", got)
	}
	if got := truncateMessage("日本語テキスト", 3); got != "日本語..." {
		t.Fatalf("pemotongan harus per rune: %q", got)
	}
}
