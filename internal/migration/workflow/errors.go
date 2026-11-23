package workflow

import "errors"

var (
	ErrNotFound   = errors.New("tidak ditemukan")
	ErrForbidden  = errors.New("akses ditolak")
	ErrImmutable  = errors.New("migration sudah pernah dieksekusi; buat migration baru")
	ErrConflict   = errors.New("sudah ada eksekusi yang terbuka untuk migration atau database ini")
	ErrNotAllowed = errors.New("aksi tidak diizinkan pada status saat ini")
)
