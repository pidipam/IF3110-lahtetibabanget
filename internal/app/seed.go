package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func SeedAdmin(ctx context.Context, store *users.Store, username, password string) (bool, error) {
	n, err := store.CountActiveAdmins(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if password == "" {
		return false, errors.New("belum ada admin, isi ADMIN_PASSWORD untuk membuat admin pertama")
	}
	username = normalizeUsername(username)
	if !usernamePattern.MatchString(username) {
		return false, errors.New("ADMIN_USERNAME tidak valid")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, fmt.Errorf("ADMIN_PASSWORD tidak valid: %w", err)
	}
	if _, err := store.Create(ctx, username, hash, true); err != nil {
		if errors.Is(err, users.ErrDuplicate) {
			return false, fmt.Errorf("username %s sudah ada tetapi bukan admin aktif", username)
		}
		return false, err
	}
	return true, nil
}
