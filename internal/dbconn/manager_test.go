package dbconn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type fakeSource struct {
	params map[int64]Params
	calls  int
}

func (f *fakeSource) Credentials(ctx context.Context, id int64) (Params, error) {
	f.calls++
	p, ok := f.params[id]
	if !ok {
		return Params{}, ErrNotFound
	}
	return p, nil
}

func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestManagerGetNotFound(t *testing.T) {
	m := NewManager(&fakeSource{params: map[int64]Params{}})
	defer m.Close()
	if _, err := m.Get(context.Background(), 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("harus ErrNotFound, dapat %v", err)
	}
}

func TestManagerGetUnreachableTarget(t *testing.T) {
	src := &fakeSource{params: map[int64]Params{1: {Host: "127.0.0.1", Port: closedPort(t), DBName: "x", Username: "u", Password: "p", SSLMode: "disable"}}}
	m := NewManager(src)
	defer m.Close()
	_, err := m.Get(context.Background(), 1)
	if !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("harus ErrConnectionFailed, dapat %v", err)
	}
	ce, ok := AsConnError(err)
	if !ok || ce.Kind != KindRefused {
		t.Fatalf("kind seharusnya refused, dapat %+v", ce)
	}
	m.Get(context.Background(), 1)
	if src.calls != 2 {
		t.Fatalf("kegagalan tidak boleh di-cache, calls=%d", src.calls)
	}
}

func TestManagerTestUnreachable(t *testing.T) {
	m := NewManager(&fakeSource{})
	p := Params{Host: "127.0.0.1", Port: closedPort(t), DBName: "x", Username: "u", Password: "supersecret", SSLMode: "disable"}
	err := m.Test(context.Background(), p)
	if !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("harus ErrConnectionFailed, dapat %v", err)
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatal("error membocorkan password")
	}
}

func TestManagerGetAfterClose(t *testing.T) {
	m := NewManager(&fakeSource{})
	m.Close()
	if _, err := m.Get(context.Background(), 1); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("harus ErrManagerClosed, dapat %v", err)
	}
}

func TestManagerInvalidateUnknownIsSafe(t *testing.T) {
	m := NewManager(&fakeSource{})
	m.Invalidate(99)
	m.Close()
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind Kind
		conn bool
	}{
		{"nil", nil, KindOther, false},
		{"canceled", context.Canceled, KindOther, false},
		{"auth", &pgconn.PgError{Code: "28P01"}, KindAuth, true},
		{"invalid auth spec", &pgconn.PgError{Code: "28000"}, KindAuth, true},
		{"db tidak ada", &pgconn.PgError{Code: "3D000"}, KindNoDatabase, true},
		{"syntax error bukan koneksi", &pgconn.PgError{Code: "42601"}, KindOther, false},
		{"refused", fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}), KindRefused, true},
		{"deadline", fmt.Errorf("wrap: %w", context.DeadlineExceeded), KindTimeout, true},
		{"dns", &net.DNSError{Err: "no such host", Name: "nope"}, KindHost, true},
		{"reset", syscall.ECONNRESET, KindOther, true},
		{"error biasa", errors.New("sesuatu"), KindOther, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, conn := classify(tc.err)
			if conn != tc.conn || (conn && kind != tc.kind) {
				t.Fatalf("classify = (%v, %v), mau (%v, %v)", kind, conn, tc.kind, tc.conn)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	if Wrap(nil) != nil {
		t.Fatal("Wrap(nil) harus nil")
	}
	plain := errors.New("biasa")
	if Wrap(plain) != plain {
		t.Fatal("error non-koneksi harus dikembalikan apa adanya")
	}
	w := Wrap(&pgconn.PgError{Code: "28P01"})
	if !errors.Is(w, ErrConnectionFailed) {
		t.Fatal("error auth harus menjadi ErrConnectionFailed")
	}
	if Wrap(w) != w {
		t.Fatal("Wrap harus idempoten")
	}
}

func TestManagerTestRespectsTimeout(t *testing.T) {
	// tidak membalas handshake.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	m := NewManager(&fakeSource{})
	m.connectTimeout = 300 * time.Millisecond
	p := Params{Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, DBName: "x", Username: "u", Password: "p", SSLMode: "disable"}
	start := time.Now()
	err = m.Test(context.Background(), p)
	if !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("harus ErrConnectionFailed, dapat %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout tidak dihormati: %v", time.Since(start))
	}
}
