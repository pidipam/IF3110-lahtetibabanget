package app

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func (a *App) actorID(r *http.Request) int64 {
	if u, ok := auth.CurrentUser(r.Context()); ok {
		return u.ID
	}
	return 0
}

func (a *App) loadGroup(w http.ResponseWriter, r *http.Request) (*users.Group, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return nil, false
	}
	g, err := a.Users.GetGroup(r.Context(), id)
	if errors.Is(err, users.ErrNotFound) {
		a.notFound(w, r)
		return nil, false
	}
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	return g, true
}

func redirectToSPA(target func(r *http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target(r), http.StatusSeeOther)
	}
}

func spaPath(path string) func(r *http.Request) string {
	return func(r *http.Request) string { return path }
}

func spaUserPath(r *http.Request) string {
	if id, ok := pathID(r, "id"); ok {
		return "/app/admin/user/" + strconv.FormatInt(id, 10)
	}
	return "/app/admin/user"
}

func (a *App) adminGroupEdit(w http.ResponseWriter, r *http.Request) {
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	a.Render.Render(w, http.StatusOK, "admin_group_edit", a.View(r, "Grup", map[string]any{"Group": g}))
}
