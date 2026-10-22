package dbconn

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound         = errors.New("koneksi database tidak ditemukan")
	ErrDuplicate        = errors.New("nama koneksi sudah dipakai")
	ErrInUse            = errors.New("koneksi masih memiliki riwayat migration")
	ErrConnectionFailed = errors.New("koneksi ke database target gagal")
)

// Aman dikirim ke template.
type Connection struct {
	ID        int64
	Name      string
	Host      string
	Port      int
	DBName    string
	Username  string
	SSLMode   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Input adalah data dari form admin. Password kosong saat update berarti dipertahankan.
type Input struct {
	Name     string
	Host     string
	Port     int
	DBName   string
	Username string
	Password string
	SSLMode  string
}

// Mengandung password, jangan dikirim ke template atau log.
type Params struct {
	Host     string
	Port     int
	DBName   string
	Username string
	Password string
	SSLMode  string
}

// String dan GoString menyembunyikan password 
func (p Params) String() string {
	return fmt.Sprintf("%s@%s:%d/%s", p.Username, p.Host, p.Port, p.DBName)
}

func (p Params) GoString() string { return p.String() }

func (p Params) DSN() string {
	q := url.Values{}
	mode := p.SSLMode
	if mode == "" {
		mode = "prefer"
	}
	q.Set("sslmode", mode)
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.Username, p.Password),
		Host:     net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:     "/" + p.DBName,
		RawQuery: q.Encode(),
	}
	return u.String()
}

var validSSLModes = map[string]bool{"disable": true, "allow": true, "prefer": true, "require": true, "verify-ca": true, "verify-full": true}

func SSLModes() []string {
	return []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
}

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func (in *Input) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.DBName = strings.TrimSpace(in.DBName)
	in.Username = strings.TrimSpace(in.Username)
	in.SSLMode = strings.TrimSpace(in.SSLMode)
	if in.SSLMode == "" {
		in.SSLMode = "prefer"
	}
}

func (in Input) Validate(requirePassword bool) error {
	bad := func(msg string) error { return &ValidationError{Msg: msg} }
	switch {
	case in.Name == "" || len(in.Name) > 64:
		return bad("Nama koneksi wajib diisi, maksimal 64 karakter.")
	case in.Host == "" || len(in.Host) > 253 || strings.ContainsAny(in.Host, " /@?#\\"):
		return bad("Host tidak valid.")
	case in.Port < 1 || in.Port > 65535:
		return bad("Port harus antara 1 dan 65535.")
	case in.DBName == "" || len(in.DBName) > 63:
		return bad("Nama database wajib diisi, maksimal 63 karakter.")
	case in.Username == "" || len(in.Username) > 63:
		return bad("Username wajib diisi, maksimal 63 karakter.")
	case requirePassword && in.Password == "":
		return bad("Password wajib diisi.")
	case len(in.Password) > 256:
		return bad("Password terlalu panjang.")
	case !validSSLModes[in.SSLMode]:
		return bad("SSL mode tidak valid.")
	}
	return nil
}

// Kind untuk klasifikasi penyebab gagal koneksi
type Kind int

const (
	KindOther Kind = iota
	KindRefused
	KindAuth
	KindTimeout
	KindNoDatabase
	KindHost
)

func (k Kind) String() string {
	switch k {
	case KindRefused:
		return "refused"
	case KindAuth:
		return "auth"
	case KindTimeout:
		return "timeout"
	case KindNoDatabase:
		return "no_database"
	case KindHost:
		return "host"
	}
	return "other"
}

func (k Kind) UserMessage() string {
	switch k {
	case KindRefused:
		return "Server database menolak koneksi. Pastikan database berjalan dan port benar."
	case KindAuth:
		return "Autentikasi ke database gagal. Periksa username dan password koneksi."
	case KindTimeout:
		return "Koneksi ke database melewati batas waktu."
	case KindNoDatabase:
		return "Database yang dituju tidak ada di server tersebut."
	case KindHost:
		return "Host database tidak ditemukan."
	}
	return "Tidak dapat terhubung ke database."
}

type ConnError struct {
	Kind Kind
	err  error
}

func (e *ConnError) Error() string { return "koneksi database gagal: " + e.Kind.String() }

func (e *ConnError) Unwrap() error { return e.err }

func (e *ConnError) Is(target error) bool { return target == ErrConnectionFailed }
