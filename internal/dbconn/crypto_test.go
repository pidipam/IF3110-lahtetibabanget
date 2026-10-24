package dbconn

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func testCipher(t *testing.T, fill byte) *Cipher {
	t.Helper()
	c, err := NewCipher(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEncryptRoundTrip(t *testing.T) {
	c := testCipher(t, 1)
	for _, plain := range []string{"", "secret", "p@ss w0rd \"'\\ üñï 漢字"} {
		enc, err := c.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.Decrypt(enc)
		if err != nil || got != plain {
			t.Fatalf("roundtrip %q: got %q, err %v", plain, got, err)
		}
	}
}

func TestEncryptHidesPlaintext(t *testing.T) {
	enc, _ := testCipher(t, 1).Encrypt("supersecret")
	if bytes.Contains([]byte(enc), []byte("supersecret")) {
		t.Fatal("ciphertext mengandung plaintext")
	}
}

func TestEncryptNonceDiffers(t *testing.T) {
	c := testCipher(t, 1)
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("dua enkripsi dari plaintext sama menghasilkan ciphertext identik")
	}
}

func TestDecryptTampered(t *testing.T) {
	c := testCipher(t, 1)
	enc, _ := c.Encrypt("secret")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0xff
	if _, err := c.Decrypt(base64.StdEncoding.EncodeToString(raw)); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("ciphertext rusak harus ErrDecrypt, dapat %v", err)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	enc, _ := testCipher(t, 1).Encrypt("secret")
	if _, err := testCipher(t, 2).Decrypt(enc); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("key salah harus ErrDecrypt, dapat %v", err)
	}
}

func TestDecryptGarbage(t *testing.T) {
	c := testCipher(t, 1)
	for _, in := range []string{"", "bukan-base64!!", "YQ==", base64.StdEncoding.EncodeToString(make([]byte, 5))} {
		if _, err := c.Decrypt(in); !errors.Is(err, ErrDecrypt) {
			t.Errorf("input %q: harus ErrDecrypt, dapat %v", in, err)
		}
	}
}

func TestNewCipherKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := NewCipher(make([]byte, n)); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("key %d byte harus ditolak", n)
		}
	}
}

func TestParseKey(t *testing.T) {
	raw := bytes.Repeat([]byte{7}, 32)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		got, err := ParseKey(enc.EncodeToString(raw))
		if err != nil || !bytes.Equal(got, raw) {
			t.Errorf("ParseKey gagal: %v", err)
		}
	}
	for _, bad := range []string{"", "pendek", base64.StdEncoding.EncodeToString(make([]byte, 16))} {
		if _, err := ParseKey(bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ParseKey(%q) harus ditolak", bad)
		}
	}
}
