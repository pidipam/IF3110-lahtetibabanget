package exec

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
)

// Class adalah klasifikasi hasil eksekusi untuk keputusan antrean.
type Class int

const (
	Success   Class = iota
	Retryable       // gagal sementara; transaksi sudah di-rollback; boleh diulang dengan jeda
	Permanent       // gagal pasti (SQL salah, revalidasi gagal); jangan diulang otomatis
	Uncertain       // hasil commit tidak pasti; periksa marker sebelum apa pun
)

func (c Class) String() string {
	return [...]string{"success", "retryable", "permanent", "uncertain"}[c]
}

// Stage membedakan kegagalan sebelum commit (aman di-rollback) dan saat commit.
type Stage int

const (
	StageRun Stage = iota
	StageCommit
)

// SQLState mengambil kode SQLSTATE bila ada ("" bila bukan error server).
func SQLState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// Classify memetakan error ke Class. Pada StageCommit, error non-server
// (koneksi putus, context batal) berarti hasil tidak pasti.
func Classify(err error, st Stage) Class {
	if err == nil {
		return Success
	}
	if code := SQLState(err); code != "" {
		switch code {
		case "40001", "40P01", "55P03", "53300", "57P01", "57P02", "57P03", "53400":
			return Retryable
		}
		if len(code) >= 2 && code[:2] == "08" {
			if st == StageCommit {
				return Uncertain
			}
			return Retryable
		}
		return Permanent
	}
	var ne net.Error
	transient := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne)
	if transient {
		if st == StageCommit {
			return Uncertain
		}
		return Retryable
	}
	if st == StageCommit {
		return Uncertain
	}
	return Permanent
}
