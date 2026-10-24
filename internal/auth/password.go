package auth

import (
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const MinPasswordLength = 8

const MaxPasswordLength = 72

var BcryptCost = 12

var ErrPasswordTooShort = errors.New("password minimal 8 karakter")

var ErrPasswordTooLong = errors.New("password maksimal 72 byte")

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), BcryptCost)
	return h
})

func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func BurnPasswordCheck(password string) {
	bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
}
