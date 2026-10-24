package app

import "net/http"

func (a *App) routes() {
	login := func(h http.HandlerFunc) http.Handler { return a.Auth.RequireLogin(h) }
	admin := func(h http.HandlerFunc) http.Handler { return a.Auth.RequireAdmin(h) }
	a.mux.HandleFunc("GET /login", a.loginPage)
	a.mux.HandleFunc("POST /login", a.login)
	a.mux.Handle("POST /logout", login(a.logout))
	a.mux.Handle("GET /{$}", login(a.home))
	a.mux.Handle("GET /admin/users", admin(a.adminUsers))
	a.mux.Handle("GET /admin/users/new", admin(a.adminUserNew))
	a.mux.Handle("POST /admin/users", admin(a.adminUserCreate))
	a.mux.Handle("GET /admin/users/{id}", admin(a.adminUserEdit))
	a.mux.Handle("POST /admin/users/{id}", admin(a.adminUserUpdate))
	a.mux.Handle("POST /admin/users/{id}/password", admin(a.adminUserPassword))
	a.mux.Handle("GET /admin/groups", admin(a.adminGroups))
	a.mux.Handle("POST /admin/groups", admin(a.adminGroupCreate))
	a.mux.Handle("GET /admin/groups/{id}", admin(a.adminGroupEdit))
	a.mux.Handle("POST /admin/groups/{id}", admin(a.adminGroupUpdate))
	a.mux.Handle("POST /admin/groups/{id}/delete", admin(a.adminGroupDelete))
	a.mux.Handle("POST /admin/groups/{id}/members", admin(a.adminGroupAddMember))
	a.mux.Handle("POST /admin/groups/{id}/members/{userID}/delete", admin(a.adminGroupRemoveMember))
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
	a.mux.HandleFunc("/", a.notFound)
}