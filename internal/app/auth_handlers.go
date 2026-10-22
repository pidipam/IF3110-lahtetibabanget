package app

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.CurrentUser(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.Render.Render(w, http.StatusOK, "login", a.View(r, "Login", map[string]any{"Username": ""}))
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := normalizeUsername(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	u, err := a.Users.GetByUsername(ctx, username)
	if err != nil && !errors.Is(err, users.ErrNotFound) {
		a.fail(w, r, err)
		return
	}
	if err != nil {
		auth.BurnPasswordCheck(password)
	}
	if err != nil || !auth.CheckPassword(u.PasswordHash, password) || !u.IsActive {
		a.Logger.Info("login failed")
		v := a.View(r, "Login", map[string]any{"Username": username})
		v.Error = "Username atau password salah."
		a.Render.Render(w, http.StatusUnauthorized, "login", v)
		return
	}
	if id := auth.FromContext(ctx); id.Session != nil {
		a.Sessions.Revoke(ctx, id.Session.ID)
	}
	token, sess, err := a.Sessions.Create(ctx, u.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.SetSessionCookie(w, token, sess.ExpiresAt)
	a.Logger.Info("login success", "user_id", u.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if err := a.Sessions.Revoke(r.Context(), id.Session.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.ClearSessionCookie(w)
	a.Logger.Info("logout", "user_id", id.User.ID)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())
	groups, err := a.Users.GroupsOfUser(r.Context(), u.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Render.Render(w, http.StatusOK, "home", a.View(r, "Beranda", map[string]any{"Groups": groups}))
}