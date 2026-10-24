// Aturan skip untuk test yang butuh database:
//   - go test -short            : selalu dilewati
//   - variabel env kosong       : dilewati dengan pesan petunjuk
//   - REQUIRE_DB=1 dan env kosong: GAGAL
package testutil

import (
	"os"
	"testing"
)

const (
	EnvTestDB    = "TEST_DATABASE_URL"
	EnvTargetDB  = "TARGET_DB_URL"
	EnvRequireDB = "REQUIRE_DB"
)

func RequireEnv(t testing.TB, name string) string {
	t.Helper()
	if testing.Short() {
		t.Skipf("-short: integration test dilewati (butuh %s)", name)
	}
	v := os.Getenv(name)
	if v == "" {
		if os.Getenv(EnvRequireDB) != "" {
			t.Fatalf("%s kosong padahal %s diset: integration test tidak boleh dilewati", name, EnvRequireDB)
		}
		t.Skipf("%s belum diisi, integration test dilewati (jalankan `make test-integration`)", name)
	}
	return v
}
