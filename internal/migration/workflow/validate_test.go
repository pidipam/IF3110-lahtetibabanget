package workflow

import (
	"errors"
	"testing"
)

func base() RevisionInput {
	return RevisionInput{Title: "t", SQLUp: "ALTER TABLE a ADD COLUMN b INT", SQLDown: "ALTER TABLE a DROP COLUMN b"}
}

func TestValidate(t *testing.T) {
	batch := func() RevisionInput {
		in := base()
		in.IsBatchMode, in.BatchColumn, in.BatchSize = true, "id", 1000
		in.SQLUp = "WITH c AS (SELECT id FROM t WHERE id > $1 ORDER BY id LIMIT $2) UPDATE t SET x=1 FROM c WHERE t.id=c.id RETURNING t.id"
		in.SQLUp = "UPDATE t SET x=1 WHERE id IN (SELECT id FROM t WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id"
		in.SQLDown = "UPDATE t SET x=0 WHERE id > $1 AND id <= $2"
		return in
	}
	cases := []struct {
		name string
		mod  func(*RevisionInput)
		in   *RevisionInput
		want error
	}{
		{"ok", func(*RevisionInput) {}, nil, nil},
		{"up kosong", func(r *RevisionInput) { r.SQLUp = " " }, nil, errors.New("any")},
		{"down wajib", func(r *RevisionInput) { r.SQLDown = "" }, nil, ErrDownRequired},
		{"irreversible tanpa alasan", func(r *RevisionInput) { r.IsIrreversible, r.SQLDown = true, "" }, nil, ErrReasonRequired},
		{"irreversible ok", func(r *RevisionInput) { r.IsIrreversible, r.SQLDown, r.IrreversibleReason = true, "", "hapus data" }, nil, nil},
		{"batch ok", func(*RevisionInput) {}, ptr(batch()), nil},
		{"batch kolom buruk", func(r *RevisionInput) { r.BatchColumn = "Id; drop" }, ptr(batch()), ErrBadBatchColumn},
		{"batch size 0", func(r *RevisionInput) { r.BatchSize = 0 }, ptr(batch()), ErrBadBatchSize},
		{"batch size besar", func(r *RevisionInput) { r.BatchSize = 100001 }, ptr(batch()), ErrBadBatchSize},
		{"batch tanpa returning", func(r *RevisionInput) { r.SQLUp = "UPDATE t SET x=1 WHERE id>$1 LIMIT $2" }, ptr(batch()), ErrBatchSQLShape},
		{"batch returning kolom lain", func(r *RevisionInput) { r.SQLUp = "UPDATE t SET x=1 WHERE id>$1 AND $2>0 RETURNING x" }, ptr(batch()), ErrBatchSQLShape},
		{"batch multi statement", func(r *RevisionInput) { r.SQLUp = "SELECT 1; UPDATE t SET x=1 WHERE id>$1 AND $2>0 RETURNING id" }, ptr(batch()), ErrBatchSQLShape},
		{"batch down tanpa param", func(r *RevisionInput) { r.SQLDown = "UPDATE t SET x=0" }, ptr(batch()), ErrBatchDownShape},
		{"batch irreversible", func(r *RevisionInput) { r.IsIrreversible, r.IrreversibleReason = true, "x" }, ptr(batch()), ErrBatchNotReversible},
	}
	for _, c := range cases {
		in := base()
		if c.in != nil {
			in = *c.in
		}
		c.mod(&in)
		err := in.Validate()
		switch {
		case c.want == nil && err != nil:
			t.Errorf("%s: tidak diharapkan error: %v", c.name, err)
		case c.want != nil && err == nil:
			t.Errorf("%s: harus error", c.name)
		case c.want != nil && c.want.Error() != "any" && !errors.Is(err, c.want):
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}
}

func ptr(r RevisionInput) *RevisionInput { return &r }

func TestChecksumSensitive(t *testing.T) {
	a := base()
	b := base()
	if a.Checksum() != b.Checksum() {
		t.Fatal("harus deterministik")
	}
	b.SQLDown += " "
	if a.Checksum() == b.Checksum() {
		t.Fatal("down berubah harus mengubah checksum")
	}
	c := base()
	c.IsBatchMode, c.BatchColumn, c.BatchSize = true, "id", 10
	d := c
	d.BatchSize = 11
	if c.Checksum() == d.Checksum() {
		t.Fatal("batch size berubah harus mengubah checksum")
	}
	if len(a.Checksum()) != 64 {
		t.Fatal("panjang harus 64")
	}
}
