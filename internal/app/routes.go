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
	a.migrationRoutes()
	a.mux.HandleFunc("/", a.notFound)
}
