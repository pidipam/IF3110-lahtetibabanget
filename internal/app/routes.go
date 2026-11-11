package app

import (
	"net/http"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/adminapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approvalapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/sessionapi"
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
	a.mux.Handle("GET /admin/groups/{id}/access", admin(a.adminGroupAccess))
	a.mux.Handle("POST /admin/groups/{id}/access", admin(a.adminGroupAccessUpdate))
	a.mux.Handle("GET /admin/connections", admin(a.adminConnections))
	a.mux.Handle("GET /admin/connections/new", admin(a.adminConnectionNew))
	a.mux.Handle("POST /admin/connections", admin(a.adminConnectionCreate))
	a.mux.Handle("POST /admin/connections/test", admin(a.adminConnectionTestNew))
	a.mux.Handle("GET /admin/connections/{id}", admin(a.adminConnectionEdit))
	a.mux.Handle("POST /admin/connections/{id}", admin(a.adminConnectionUpdate))
	a.mux.Handle("POST /admin/connections/{id}/test", admin(a.adminConnectionTestEdit))
	a.mux.Handle("POST /admin/connections/{id}/delete", admin(a.adminConnectionDelete))
	a.mux.Handle("GET /databases", login(a.databases))
	a.mux.Handle("GET /databases/{id}", login(a.databaseView))
	a.mux.Handle("GET /databases/{id}/tables/{schema}/{table}", login(a.databaseTable))
	a.migrationRoutes()
	a.schemaDiffRoutes()
	a.workflowRoutes()
	(&sessionapi.Handler{Users: a.Users, Sessions: a.Sessions, Auth: a.Auth, Authz: a.Authz, Conns: a.Conns, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	(&adminapi.Handler{Users: a.Users, Sessions: a.Sessions, Auth: a.Auth, Grants: a.Grants, GrantStore: a.GrantStore, Conns: a.Conns, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireAdmin)
	(&approvalapi.Handler{Approvals: a.Approvals, Authz: a.Authz, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	a.mux.HandleFunc("/", a.notFound)
}
