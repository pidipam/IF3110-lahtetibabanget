package app

import (
	"github.com/pidipam/IF3110-lahtetibabanget/internal/adminapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approvalapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/connectionapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/explainapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/schemadiffapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/sessionapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/viewerapi"
)

func (a *App) routes() {
	a.oldPageRoutes()
	a.workflowRoutes()
	(&sessionapi.Handler{Users: a.Users, Sessions: a.Sessions, Auth: a.Auth, Authz: a.Authz, Conns: a.Conns, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	(&adminapi.Handler{Users: a.Users, Sessions: a.Sessions, Auth: a.Auth, Grants: a.Grants, GrantStore: a.GrantStore, Conns: a.Conns, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireAdmin)
	(&connectionapi.Handler{Conns: a.Conns, Manager: a.Manager, Grants: a.GrantStore, Sessions: a.Sessions, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireAdmin)
	(&viewerapi.Handler{Conns: a.Conns, Manager: a.Manager, Authz: a.Authz, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	(&approvalapi.Handler{Approvals: a.Approvals, Authz: a.Authz, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	(&explainapi.Handler{Migrations: a.Workflow, Explainer: a.migrationExec, Authz: a.Authz, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	(&schemadiffapi.Handler{Conns: a.Conns, Pools: a.Manager, Authz: a.Authz, Logger: a.Logger}).RegisterRoutes(a.mux, a.Auth.RequireLogin)
	a.mux.HandleFunc("/", a.notFound)
}
