package app

import (
	"net/http"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/adminapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approvalapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/connectionapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/sessionapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/viewerapi"
)

func (a *App) routes() {
	login := func(h http.HandlerFunc) http.Handler { return a.Auth.RequireLogin(h) }
	admin := func(h http.HandlerFunc) http.Handler { return a.Auth.RequireAdmin(h) }
	a.mux.HandleFunc("GET /login", a.loginPage)
	a.mux.HandleFunc("POST /login", a.login)
	a.mux.Handle("POST /logout", login(a.logout))
	a.mux.Handle("GET /{$}", login(a.home))
	a.mux.Handle("GET /admin/users", admin(redirectToSPA(spaPath("/app/admin/user"))))
	a.mux.Handle("GET /admin/users/new", admin(redirectToSPA(spaPath("/app/admin/user/baru"))))
	a.mux.Handle("GET /admin/users/{id}", admin(redirectToSPA(spaUserPath)))
	a.mux.Handle("GET /admin/groups", admin(redirectToSPA(spaPath("/app/admin/grup"))))
	a.mux.Handle("GET /admin/groups/{id}", admin(a.adminGroupEdit))
	a.migrationRoutes()
	a.schemaDiffRoutes()
	a.workflowRoutes()
	(&sessionapi.Handler{Users: a.Users, Sessions: a.Sessions, Auth: a.Auth, Authz: a.Authz, Conns: a.Conns, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	(&adminapi.Handler{Users: a.Users, Sessions: a.Sessions, Auth: a.Auth, Grants: a.Grants, GrantStore: a.GrantStore, Conns: a.Conns, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireAdmin)
	(&connectionapi.Handler{Conns: a.Conns, Manager: a.Manager, Grants: a.GrantStore, Sessions: a.Sessions, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireAdmin)
	(&viewerapi.Handler{Conns: a.Conns, Manager: a.Manager, Authz: a.Authz, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	(&approvalapi.Handler{Approvals: a.Approvals, Authz: a.Authz, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	a.mux.HandleFunc("/", a.notFound)
}
