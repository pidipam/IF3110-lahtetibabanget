package workflow

import "errors"

var (
	ErrNotNextInLine    = errors.New("hanya migration berikutnya dalam urutan yang boleh dijalankan")
	ErrNotLastActive    = errors.New("rollback hanya untuk migration aktif terakhir")
	ErrIrreversible     = errors.New("migration irreversible tidak dapat di-rollback")
	ErrNotSubmitted     = errors.New("migration belum memiliki urutan")
	ErrAlreadyActive    = errors.New("migration sudah aktif")
	ErrNotActive        = errors.New("migration tidak aktif")
	ErrForwardNeedsUndo = errors.New("forward hanya untuk migration yang sudah di-rollback")
)

// Item adalah ringkasan satu migration untuk perhitungan posisi.
// Order = 0 berarti belum diajukan. Active = eksekusi sukses terakhirnya
// adalah up/forward (bukan rollback).
type Item struct {
	ID     int64
	Order  int
	Active bool
	// EverRun: pernah sukses dijalankan (dipakai untuk membedakan up vs forward).
	EverRun bool
}

// NextInLine mengembalikan migration terdekat yang sudah diajukan dan belum aktif.
func NextInLine(items []Item) (Item, bool) {
	var best Item
	found := false
	for _, it := range items {
		if it.Order == 0 || it.Active {
			continue
		}
		if !found || it.Order < best.Order {
			best, found = it, true
		}
	}
	return best, found
}

// LastActive mengembalikan migration aktif dengan urutan tertinggi.
func LastActive(items []Item) (Item, bool) {
	var best Item
	found := false
	for _, it := range items {
		if !it.Active {
			continue
		}
		if !found || it.Order > best.Order {
			best, found = it, true
		}
	}
	return best, found
}

// CheckRun memeriksa apakah eksekusi `kind` atas migration `id` sah.
// reversible hanya dipakai untuk rollback.
func CheckRun(kind Kind, id int64, reversible bool, items []Item) error {
	var target *Item
	for i := range items {
		if items[i].ID == id {
			target = &items[i]
		}
	}
	if target == nil || target.Order == 0 {
		return ErrNotSubmitted
	}
	switch kind {
	case KindUp, KindForward:
		if target.Active {
			return ErrAlreadyActive
		}
		if kind == KindForward && !target.EverRun {
			return ErrForwardNeedsUndo
		}
		if kind == KindUp && target.EverRun {
			return ErrForwardNeedsUndo // sudah pernah jalan: gunakan forward
		}
		next, ok := NextInLine(items)
		if !ok || next.ID != id {
			return ErrNotNextInLine
		}
	case KindRollback:
		if !target.Active {
			return ErrNotActive
		}
		last, _ := LastActive(items)
		if last.ID != id {
			return ErrNotLastActive
		}
		if !reversible {
			return ErrIrreversible
		}
	default:
		return ErrInvalidTransition
	}
	return nil
}
