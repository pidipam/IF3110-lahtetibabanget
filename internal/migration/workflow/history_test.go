package workflow

import "testing"

func i64(v int64) *int64 { return &v }

// Contoh spesifikasi: A[E1] B[E2] B-rollback[E3] B-forward[E4] C[E5]
func specExample() []HistoryEntry {
	return []HistoryEntry{
		{ExecutionID: 1, MigrationTitle: "A", Kind: KindUp, Status: StatusSuccess},
		{ExecutionID: 2, MigrationTitle: "B", Kind: KindUp, Status: StatusSuccess},
		{ExecutionID: 3, MigrationTitle: "B", Kind: KindRollback, Status: StatusSuccess, RevertedExecutionID: i64(2)},
		{ExecutionID: 4, MigrationTitle: "B", Kind: KindForward, Status: StatusSuccess},
		{ExecutionID: 5, MigrationTitle: "C", Kind: KindUp, Status: StatusSuccess},
	}
}

func ids(h []HistoryEntry) []int64 {
	var o []int64
	for _, e := range h {
		o = append(o, e.ExecutionID)
	}
	return o
}

func TestHideRollbackSpecExample(t *testing.T) {
	got := ids(HideRollback(specExample()))
	want := []int64{1, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestHideRollbackPairsByIDNotName(t *testing.T) {
	// dua migration bernama sama; hanya pasangan ID yang disembunyikan
	in := []HistoryEntry{
		{ExecutionID: 10, MigrationTitle: "X", Kind: KindUp, Status: StatusSuccess},
		{ExecutionID: 11, MigrationTitle: "X", Kind: KindUp, Status: StatusSuccess},
		{ExecutionID: 12, MigrationTitle: "X", Kind: KindRollback, Status: StatusSuccess, RevertedExecutionID: i64(11)},
	}
	got := ids(HideRollback(in))
	if len(got) != 1 || got[0] != 10 {
		t.Fatalf("got %v", got)
	}
}

func TestHideRollbackNoRollbackKeepsAll(t *testing.T) {
	in := specExample()[:2]
	if len(HideRollback(in)) != 2 {
		t.Fatal("tanpa rollback tidak ada yang disembunyikan")
	}
	if len(HideRollback(nil)) != 0 {
		t.Fatal("nil aman")
	}
}
