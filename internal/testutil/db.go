package testutil

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/platformdb"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
	"golang.org/x/crypto/bcrypt"
)

const testDBLock = 7310099

var TestKey = []byte("0123456789abcdef0123456789abcdef")

func NewTestDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dsn := RequireEnv(t, EnvTestDB)
	ctx := context.Background()
	auth.BcryptCost = bcrypt.MinCost 

	db, err := platformdb.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("konek ke test DB: %v", err)
	}
	t.Cleanup(db.Close)

	lock, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(testDBLock)); err != nil {
		lock.Release()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		lock.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(testDBLock))
		lock.Release()
	})

	if err := platformdb.Migrate(ctx, db); err != nil {
		t.Fatalf("migrasi: %v", err)
	}
	truncateAll(t, db)
	return db
}

func truncateAll(t testing.TB, db *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	rows, err := db.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations'")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		names = append(names, pgx.Identifier{n}.Sanitize())
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		return
	}
	if _, err := db.Exec(ctx, "TRUNCATE "+strings.Join(names, ", ")+" RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("mengosongkan tabel: %v", err)
	}
}

func Count(t testing.TB, db *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var seq atomic.Int64

func SeedUser(t testing.TB, db *pgxpool.Pool, username, password string, admin bool) int64 {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.NewStore(db).Create(context.Background(), username, hash, admin)
	if err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	return u.ID
}

func SeedGroup(t testing.TB, db *pgxpool.Pool, name string) int64 {
	t.Helper()
	g, err := users.NewStore(db).CreateGroup(context.Background(), name, "")
	if err != nil {
		t.Fatalf("seed group %q: %v", name, err)
	}
	return g.ID
}

func SeedMember(t testing.TB, db *pgxpool.Pool, groupID, userID int64) {
	t.Helper()
	if err := users.NewStore(db).AddMember(context.Background(), groupID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}

func NewCipher(t testing.TB) *dbconn.Cipher {
	t.Helper()
	c, err := dbconn.NewCipher(TestKey)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Default: target-db sampel (127.0.0.1:5435)
func SeedConnection(t testing.TB, db *pgxpool.Pool, opts ...func(*dbconn.Input)) *dbconn.Connection {
	t.Helper()
	in := dbconn.Input{
		Name:     "conn-" + strconv.FormatInt(seq.Add(1), 10),
		Host:     "127.0.0.1",
		Port:     5435,
		DBName:   "sample",
		Username: "app",
		Password: "app",
		SSLMode:  "disable",
	}
	for _, o := range opts {
		o(&in)
	}
	c, err := dbconn.NewStore(db, NewCipher(t)).Create(context.Background(), in)
	if err != nil {
		t.Fatalf("seed koneksi: %v", err)
	}
	return c
}

func SeedGrant(t testing.TB, db *pgxpool.Pool, groupID, connID int64, level authz.Level) {
	t.Helper()
	if _, err := dbconn.NewGrantStore(db).Set(context.Background(), groupID, connID, level); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
}
