package app

import (
	"net/http"
	"strconv"
	"strings"
)

var oldPages = map[string]string{
	"GET /{$}": "/app/",
	"GET /login": "/app/login",
	"GET /admin/users": "/app/admin/user",
	"GET /admin/users/new": "/app/admin/user/baru",
	"GET /admin/users/{id}": "/app/admin/user/{id}",
	"GET /admin/groups": "/app/admin/grup",
	"GET /admin/groups/{id}": "/app/admin/grup/{id}",
	"GET /admin/groups/{id}/access": "/app/admin/grup/{id}",
	"GET /admin/connections": "/app/admin/koneksi",
	"GET /admin/connections/new": "/app/admin/koneksi/baru",
	"GET /admin/connections/{id}": "/app/admin/koneksi/{id}",
	"GET /databases": "/app/database",
	"GET /databases/{id}": "/app/database/{id}",
	"GET /databases/{id}/schema-diff": "/app/migration/database/{id}/schema-diff",
	"GET /databases/{id}/migrations": "/app/migration/database/{id}",
	"GET /databases/{id}/migrations/new": "/app/migration/database/{id}/baru",
	"GET /databases/{id}/migrations/{mid}": "/app/migration/{mid}",
	"GET /databases/{id}/migrations/{mid}/edit": "/app/migration/{mid}/ubah",
	"GET /databases/{id}/migrations/{mid}/review": "/app/migration/{mid}",
}

func (a *App) oldPageRoutes() {
	for pattern, target := range oldPages {
		a.mux.HandleFunc(pattern, a.redirectTo(target))
	}
}

func (a *App) redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		to := target
		for _, name := range []string{"id", "mid"} {
			if !strings.Contains(to, "{"+name+"}") {
				continue
			}
			id, ok := pathID(r, name)
			if !ok {
				a.notFound(w, r)
				return
			}
			to = strings.ReplaceAll(to, "{"+name+"}", strconv.FormatInt(id, 10))
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}
