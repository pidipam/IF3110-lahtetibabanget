package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
)

var (
	ErrDownRequired       = errors.New("sql down wajib diisi, atau tandai irreversible beserta alasannya")
	ErrReasonRequired     = errors.New("alasan irreversible wajib diisi")
	ErrBadBatchColumn     = errors.New("kolom batch tidak valid")
	ErrBadBatchSize       = errors.New("ukuran batch harus 1..100000")
	ErrBatchSQLShape      = errors.New("sql batch harus satu statement dengan $1, $2, dan RETURNING kolom batch")
	ErrBatchDownShape     = errors.New("sql down batch harus memakai $1 (batas bawah) dan $2 (batas atas)")
	ErrBatchNotReversible = errors.New("migration batch tidak boleh irreversible tanpa down; isi sql down")
)

const MaxBatchSize = 100_000

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// RevisionInput adalah isi yang dapat diisi pengguna untuk sebuah revisi.
type RevisionInput struct {
	Title              string
	SQLUp              string
	SQLDown            string
	IsIrreversible     bool
	IrreversibleReason string
	IsBatchMode        bool
	BatchColumn        string
	BatchSize          int
}

// Validate memeriksa seluruh aturan revisi. Judul diperiksa terpisah oleh pemanggil
// bila hanya revisi yang berubah (lihat ValidateTitle).
func (in RevisionInput) Validate() error {
	if err := checkSQL(in.SQLUp); err != nil {
		return err
	}
	if in.IsIrreversible {
		if strings.TrimSpace(in.IrreversibleReason) == "" {
			return ErrReasonRequired
		}
	} else {
		if strings.TrimSpace(in.SQLDown) == "" {
			return ErrDownRequired
		}
		if err := checkSQL(in.SQLDown); err != nil {
			return err
		}
	}
	if !in.IsBatchMode {
		return nil
	}
	if !identRe.MatchString(in.BatchColumn) {
		return ErrBadBatchColumn
	}
	if in.BatchSize < 1 || in.BatchSize > MaxBatchSize {
		return ErrBadBatchSize
	}
	if in.IsIrreversible {
		return ErrBatchNotReversible
	}
	if !batchUpShape(in.SQLUp, in.BatchColumn) {
		return ErrBatchSQLShape
	}
	if !strings.Contains(in.SQLDown, "$1") || !strings.Contains(in.SQLDown, "$2") || hasSemicolonMid(in.SQLDown) {
		return ErrBatchDownShape
	}
	return nil
}

// hasSemicolonMid: true bila ada ';' selain penutup di akhir (multi-statement).
func hasSemicolonMid(s string) bool {
	t := strings.TrimRight(strings.TrimSpace(s), ";")
	return strings.Contains(t, ";")
}

func batchUpShape(sql, col string) bool {
	if hasSemicolonMid(sql) {
		return false
	}
	if !strings.Contains(sql, "$1") || !strings.Contains(sql, "$2") {
		return false
	}
	low := strings.ToLower(sql)
	i := strings.LastIndex(low, "returning")
	if i < 0 {
		return false
	}
	ret := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(low[i+len("returning"):]), ";"))
	return ret == col
}

// Checksum mengikat isi yang disetujui: SQL up/down dan konfigurasi batch.
func (in RevisionInput) Checksum() string {
	h := sha256.New()
	w := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	w(in.SQLUp)
	w(in.SQLDown)
	if in.IsIrreversible {
		w("irreversible")
	}
	if in.IsBatchMode {
		w("batch")
		w(in.BatchColumn)
		w(string(rune(in.BatchSize)))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Validator M1 mengembalikan (nilai-rapi, error); workflow hanya butuh error-nya.
func checkTitle(s string) error { _, err := migration.ValidateTitle(s); return err }
func checkSQL(s string) error   { _, err := migration.ValidateSQL(s); return err }
