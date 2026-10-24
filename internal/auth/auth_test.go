package auth

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func init() {
	BcryptCost = bcrypt.MinCost
}

func TestHashPasswordNotPlaintext(t *testing.T) {
	hash, err := HashPassword("rahasia123")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "rahasia123" || strings.Contains(hash, "rahasia123") {
		t.Fatal("hash memuat password asli")
	}
	if !CheckPassword(hash, "rahasia123") {
		t.Fatal("password benar ditolak")
	}
	if CheckPassword(hash, "salah12345") {
		t.Fatal("password salah diterima")
	}
}

func TestHashPasswordSaltedDifferently(t *testing.T) {
	a, _ := HashPassword("rahasia123")
	b, _ := HashPassword("rahasia123")
	if a == b {
		t.Fatal("dua hash dari password sama seharusnya berbeda")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("pendek"); err != ErrPasswordTooShort {
		t.Fatalf("want ErrPasswordTooShort, got %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 73)); err != ErrPasswordTooLong {
		t.Fatalf("want ErrPasswordTooLong, got %v", err)
	}
	if err := ValidatePassword("cukuppanjang"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestNewTokenRandomAndLongEnough(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tok, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) != 43 {
			t.Fatalf("panjang token %d, want 43", len(tok))
		}
		if seen[tok] {
			t.Fatal("token duplikat")
		}
		seen[tok] = true
	}
}

func TestHashTokenDeterministicAndHidesToken(t *testing.T) {
	tok, _ := NewToken()
	a := HashToken(tok)
	b := HashToken(tok)
	if string(a) != string(b) {
		t.Fatal("hash token tidak deterministik")
	}
	if strings.Contains(string(a), tok) {
		t.Fatal("hash memuat token asli")
	}
}

func TestTokensEqual(t *testing.T) {
	if !TokensEqual("abc", "abc") {
		t.Fatal("token sama dianggap beda")
	}
	if TokensEqual("abc", "abd") || TokensEqual("abc", "ab") || TokensEqual("", "") {
		t.Fatal("token beda atau kosong dianggap sama")
	}
}

var testCfg = SessionConfig{IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 8 * time.Hour, RotateInterval: 15 * time.Minute, GracePeriod: 30 * time.Second}

func TestCheckSession(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	base := Session{CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Minute), ExpiresAt: now.Add(7 * time.Hour)}
	cases := []struct {
		name string
		edit func(s *Session)
		want error
	}{{"valid", func(s *Session) {}, nil}, {"idle", func(s *Session) { s.LastSeenAt = now.Add(-31 * time.Minute) }, ErrSessionExpired}, {"absolute", func(s *Session) { s.ExpiresAt = now }, ErrSessionExpired}, {"revoked", func(s *Session) { s.Revoked = true }, ErrSessionExpired}}
	for _, c := range cases {
		s := base
		c.edit(&s)
		if got := CheckSession(&s, now, testCfg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNeedsRotation(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	fresh := &Session{RotatedAt: now.Add(-time.Minute), IsCurrent: true}
	old := &Session{RotatedAt: now.Add(-16 * time.Minute), IsCurrent: true}
	forced := &Session{RotatedAt: now, ForceRotate: true, IsCurrent: true}
	previous := &Session{RotatedAt: now.Add(-time.Hour), IsCurrent: false}
	if NeedsRotation(fresh, now, testCfg) {
		t.Error("session baru tidak perlu rotasi")
	}
	if !NeedsRotation(old, now, testCfg) {
		t.Error("session lama perlu rotasi berkala")
	}
	if !NeedsRotation(forced, now, testCfg) {
		t.Error("force_rotate harus memicu rotasi")
	}
	if NeedsRotation(previous, now, testCfg) {
		t.Error("identifier lama dalam grace period tidak boleh memicu rotasi")
	}
}

func TestValidCSRF(t *testing.T) {
	form := url.Values{CSRFField: {"token-benar"}}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !ValidCSRF("token-benar", r) {
		t.Fatal("token form benar ditolak")
	}
	r = httptest.NewRequest("POST", "/", nil)
	r.Header.Set(CSRFHeader, "token-benar")
	if !ValidCSRF("token-benar", r) {
		t.Fatal("token header benar ditolak")
	}
	r = httptest.NewRequest("POST", "/", nil)
	if ValidCSRF("token-benar", r) {
		t.Fatal("request tanpa token diterima")
	}
	r = httptest.NewRequest("POST", "/", nil)
	r.Header.Set(CSRFHeader, "token-salah")
	if ValidCSRF("token-benar", r) {
		t.Fatal("token salah diterima")
	}
}

func TestIsSafeMethod(t *testing.T) {
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		if !IsSafeMethod(m) {
			t.Errorf("%s seharusnya aman", m)
		}
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if IsSafeMethod(m) {
			t.Errorf("%s seharusnya mengubah state", m)
		}
	}
}
