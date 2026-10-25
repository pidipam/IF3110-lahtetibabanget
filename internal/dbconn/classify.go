package dbconn

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

func classify(err error) (Kind, bool) {
	if err == nil || errors.Is(err, context.Canceled) {
		return KindOther, false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "28P01", "28000":
			return KindAuth, true
		case "3D000":
			return KindNoDatabase, true
		case "57P03", "53300":
			return KindRefused, true
		}
		return KindOther, false
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return KindHost, true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.Errno(10061)) {
		return KindRefused, true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return KindTimeout, true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return KindTimeout, true
	}
	var ce *pgconn.ConnectError
	if errors.As(err, &ce) {
		return KindOther, true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return KindOther, true
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return KindOther, true
	}
	return KindOther, false
}

func Wrap(err error) error {
	if err == nil {
		return nil
	}
	var ce *ConnError
	if errors.As(err, &ce) {
		return err
	}
	if kind, ok := classify(err); ok {
		return &ConnError{Kind: kind, err: err}
	}
	return err
}

func AsConnError(err error) (*ConnError, bool) {
	var ce *ConnError
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}
