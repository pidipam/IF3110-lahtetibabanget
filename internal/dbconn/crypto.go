package dbconn

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const keySize = 32

var (
	ErrInvalidKey = errors.New("APP_ENCRYPTION_KEY harus berupa base64 dari tepat 32 byte")
	ErrDecrypt    = errors.New("gagal mendekripsi data")
)

// Format ciphertext: base64(nonce || sealed).
type Cipher struct {
	aead cipher.AEAD
}

// Contoh membuat key: openssl rand -base64 32
func ParseKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == keySize {
			return b, nil
		}
	}
	return nil, ErrInvalidKey
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (c *Cipher) Decrypt(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", ErrDecrypt
	}
	n := c.aead.NonceSize()
	if len(raw) < n+c.aead.Overhead() {
		return "", ErrDecrypt
	}
	plain, err := c.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}
