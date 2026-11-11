package workflow

import (
	"errors"
	"testing"
)

// A(1) B(2) C(3)
func abc(a, b, c bool) []Item {
	return []Item{
		{ID: 1, Order: 1, Active: a, EverRun: a},
		{ID: 2, Order: 2, Active: b, EverRun: b},
		{ID: 3, Order: 3, Active: c, EverRun: c},
	}
}

func TestNextAndLast(t *testing.T) {
	n, _ := NextInLine(abc(false, false, false))
	if n.ID != 1 {
		t.Fatal("A harus berikutnya")
	}
	n, _ = NextInLine(abc(true, false, false))
	if n.ID != 2 {
		t.Fatal("B harus berikutnya")
	}
	if _, ok := NextInLine(abc(true, true, true)); ok {
		t.Fatal("tidak ada lagi")
	}
	l, _ := LastActive(abc(true, true, false))
	if l.ID != 2 {
		t.Fatal("B aktif terakhir")
	}
	if _, ok := LastActive(abc(false, false, false)); ok {
		t.Fatal("tidak ada aktif")
	}
}

func TestCheckRun(t *testing.T) {
	if err := CheckRun(KindUp, 1, true, abc(false, false, false)); err != nil {
		t.Fatal(err)
	}
	if err := CheckRun(KindUp, 2, true, abc(false, false, false)); !errors.Is(err, ErrNotNextInLine) {
		t.Fatalf("B tak boleh mendahului A: %v", err)
	}
	// rollback C harus sebelum B
	if err := CheckRun(KindRollback, 2, true, abc(true, true, true)); !errors.Is(err, ErrNotLastActive) {
		t.Fatalf("rollback B sebelum C harus ditolak: %v", err)
	}
	if err := CheckRun(KindRollback, 3, true, abc(true, true, true)); err != nil {
		t.Fatal(err)
	}
	if err := CheckRun(KindRollback, 3, false, abc(true, true, true)); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("irreversible: %v", err)
	}
	if err := CheckRun(KindRollback, 3, true, abc(true, true, false)); !errors.Is(err, ErrNotActive) {
		t.Fatalf("C belum aktif: %v", err)
	}
	// B di-rollback: B otomatis berikutnya, forward B sah, forward C tidak.
	items := []Item{{1, 1, true, true}, {2, 2, false, true}, {3, 3, true, true}}
	_ = items
	items = []Item{{1, 1, true, true}, {2, 2, false, true}, {3, 3, false, true}}
	if err := CheckRun(KindForward, 2, true, items); err != nil {
		t.Fatalf("forward B: %v", err)
	}
	if err := CheckRun(KindForward, 3, true, items); !errors.Is(err, ErrNotNextInLine) {
		t.Fatalf("forward C harus menunggu B: %v", err)
	}
	if err := CheckRun(KindUp, 2, true, items); err == nil {
		t.Fatal("up untuk yang pernah jalan harus lewat forward")
	}
	if err := CheckRun(KindForward, 1, true, abc(true, false, false)); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("A aktif: %v", err)
	}
	if err := CheckRun(KindUp, 9, true, abc(false, false, false)); !errors.Is(err, ErrNotSubmitted) {
		t.Fatalf("tak dikenal: %v", err)
	}
	un := []Item{{ID: 1, Order: 0}}
	if err := CheckRun(KindUp, 1, true, un); !errors.Is(err, ErrNotSubmitted) {
		t.Fatalf("belum diajukan: %v", err)
	}
}
