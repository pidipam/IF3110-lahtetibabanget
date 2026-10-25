package dbconn_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

func TestMariaDBManagerConnectsAndRejectsBadPassword(t *testing.T) {
	db := testutil.NewTestDB(t)
	maria := testutil.MariaDB(t)
	good := testutil.SeedConnection(t, db, maria)
	bad := testutil.SeedConnection(t, db, maria, func(in *dbconn.Input) { in.Password = "salah" })

	m := dbconn.NewManager(dbconn.NewStore(db, testutil.NewCipher(t)))
	defer m.Close()
	ctx := context.Background()

	sdb, err := m.GetSQL(ctx, good.ID)
	if err != nil {
		t.Fatalf("GetSQL: %v", err)
	}
	var n int
	if err := sdb.QueryRowContext(ctx, "SELECT COUNT(*) FROM customers").Scan(&n); err != nil || n == 0 {
		t.Fatalf("query sampel gagal: n=%d err=%v", n, err)
	}
	if _, err := m.Get(ctx, good.ID); !errors.Is(err, dbconn.ErrWrongEngine) {
		t.Fatalf("Get pada MariaDB harus ErrWrongEngine, dapat %v", err)
	}
	_, err = m.GetSQL(ctx, bad.ID)
	if ce, ok := dbconn.AsConnError(err); !ok || ce.Kind != dbconn.KindAuth {
		t.Fatalf("password salah harus KindAuth, dapat %v", err)
	}
}
