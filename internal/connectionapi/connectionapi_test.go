package connectionapi

import (
	"testing"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

func TestFieldOf(t *testing.T) {
	cases := map[string]string{
		"Nama koneksi wajib diisi.":      "name",
		"Host tidak valid.":              "host",
		"Port harus antara 1 dan 65535.": "port",
		"Nama database wajib diisi.":     "db_name",
		"Username wajib diisi.":          "username",
		"Password wajib diisi.":          "password",
		"SSL mode tidak valid.":          "sslmode",
		"Engine database tidak valid.":   "engine",
		"tidak dikenal":                  "input",
	}
	for msg, want := range cases {
		if got := fieldOf(msg); got != want {
			t.Fatalf("fieldOf(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestToDTOTanpaPassword(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	c := &dbconn.Connection{ID: 7, Name: "utama", Engine: dbconn.EnginePostgres, Host: "db.internal", Port: 5432, DBName: "app", Username: "app", SSLMode: "disable", CreatedAt: now, UpdatedAt: now}
	got := toDTO(c)
	if got.ID != 7 || got.Name != "utama" || got.Host != "db.internal" || got.Port != 5432 || got.DBName != "app" || got.Username != "app" || got.SSLMode != "disable" {
		t.Fatalf("dto: %+v", got)
	}
	if got.CreatedAt != now || got.UpdatedAt != now {
		t.Fatalf("waktu: %+v", got)
	}
}
