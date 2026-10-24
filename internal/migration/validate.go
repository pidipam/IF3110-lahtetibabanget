package migration

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxSQLBytes   = 100_000
	MaxTitleRunes = 100
)

var (
	ErrEmptyTitle   = errors.New("judul wajib diisi")
	ErrTitleTooLong = errors.New("judul maksimal 100 karakter")
	ErrEmptySQL     = errors.New("SQL wajib diisi")
	ErrSQLTooLong   = errors.New("SQL terlalu panjang (maksimal 100 KB)")
	ErrInvalidSQL   = errors.New("SQL mengandung karakter tidak valid")
	ErrTxControl    = errors.New("SQL tidak boleh berisi BEGIN, COMMIT, ROLLBACK, atau SAVEPOINT karena transaksi dikelola platform")
)

var (
	dollarTag = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)
	txControl = regexp.MustCompile(`(?i)(^|;)\s*(begin|commit|end|rollback|abort|savepoint|release|start\s+transaction)\b`)
)

func ValidateTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ErrEmptyTitle
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		return "", ErrTitleTooLong
	}
	return title, nil
}

func ValidateSQL(sql string) (string, error) {
	sql = strings.TrimSpace(sql)
	if len(sql) > MaxSQLBytes {
		return "", ErrSQLTooLong
	}
	if strings.ContainsRune(sql, 0) || !utf8.ValidString(sql) {
		return "", ErrInvalidSQL
	}
	stripped := stripNoise(sql)
	if strings.Trim(stripped, " \t\r\n;") == "" {
		return "", ErrEmptySQL
	}
	if txControl.MatchString(stripped) {
		return "", ErrTxControl
	}
	return sql, nil
}

// stripNoise membuang komentar dan mengosongkan isi string literal serta
// blok dollar-quoted supaya pemeriksaan kata kunci tidak salah menangkap isinya.
func stripNoise(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		switch {
		case strings.HasPrefix(s[i:], "--"):
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case strings.HasPrefix(s[i:], "/*"):
			if end := strings.Index(s[i+2:], "*/"); end < 0 {
				i = len(s)
			} else {
				i += end + 4
			}
			b.WriteByte(' ')
		case s[i] == '\'':
			i++
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
			b.WriteString("''")
		case s[i] == '$' && dollarTag.MatchString(s[i:]):
			tag := dollarTag.FindString(s[i:])
			if end := strings.Index(s[i+len(tag):], tag); end < 0 {
				i = len(s)
			} else {
				i += len(tag) + end + len(tag)
			}
			b.WriteString("''")
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}