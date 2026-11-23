package schemadiffapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/schemadiff"
)

type Conns interface {
	Get(ctx context.Context, id int64) (*dbconn.Connection, error)
}

type Pools interface {
	Get(ctx context.Context, id int64) (*pgxpool.Pool, error)
}

type Authorizer interface {
	Can(ctx context.Context, p authz.Principal, dbID int64, a authz.Action) error
}

type Handler struct {
	Conns Conns
	Pools Pools
	Authz Authorizer
	Logger *slog.Logger
}

type sourceDTO struct {
	DatabaseID int64 `json:"database_id"`
	DatabaseName string `json:"database_name"`
	Schemas []string `json:"schemas"`
	Schema string `json:"schema"`
	CurrentDDL string `json:"current_ddl"`
}

type previewRequest struct {
	Schema string `json:"schema"`
	DesiredDDL string `json:"desired_ddl"`
	IncludeDestructive bool `json:"include_destructive"`
}

type changeDTO struct {
	Op string `json:"op"`
	OpLabel string `json:"op_label"`
	Kind string `json:"kind"`
	Object string `json:"object"`
	Detail string `json:"detail"`
	Destructive bool `json:"destructive"`
}

type resultDTO struct {
	Schema string `json:"schema"`
	Title string `json:"title"`
	Changes []changeDTO `json:"changes"`
	Warnings []string `json:"warnings"`
	SQL string `json:"sql"`
	Statements int `json:"statements"`
	Skipped int `json:"skipped"`
	TooLarge bool `json:"too_large"`
	CanCreate bool `json:"can_create"`
}

type scope struct {
	p authz.Principal
	conn *dbconn.Connection
	pool *pgxpool.Pool
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireLogin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/databases/{id}/schema-diff", requireLogin(http.HandlerFunc(h.source)))
	mux.Handle("POST /api/databases/{id}/schema-diff", requireLogin(http.HandlerFunc(h.preview)))
}

func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (scope, bool) {
	ctx := r.Context()
	p, ok := auth.CurrentPrincipal(ctx)
	if !ok {
		httpapi.Unauthenticated(w, false)
		return scope{}, false
	}
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return scope{}, false
	}
	if err := h.Authz.Can(ctx, p, id, authz.EditMigration); err != nil {
		h.fail(w, r, err)
		return scope{}, false
	}
	c, err := h.Conns.Get(ctx, id)
	if err != nil {
		h.fail(w, r, err)
		return scope{}, false
	}
	if dbconn.IsMySQLFamily(c.Engine) {
		httpapi.WriteError(w, http.StatusUnprocessableEntity, "schema_diff_unsupported", "Schema diff hanya tersedia untuk PostgreSQL.", nil)
		return scope{}, false
	}
	pool, err := h.Pools.Get(ctx, c.ID)
	if err != nil {
		h.fail(w, r, err)
		return scope{}, false
	}
	return scope{p: p, conn: c, pool: pool}, true
}

func (h *Handler) source(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	schemas, err := schemadiff.Schemas(ctx, sc.pool)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := sourceDTO{DatabaseID: sc.conn.ID, DatabaseName: sc.conn.Name, Schemas: schemas}
	if out.Schemas == nil {
		out.Schemas = []string{}
	}
	out.Schema = strings.TrimSpace(r.URL.Query().Get("schema"))
	if out.Schema == "" {
		out.Schema = preferredSchema(schemas)
	}
	if out.Schema == "" {
		httpapi.WriteJSON(w, http.StatusOK, out)
		return
	}
	cur, err := schemadiff.Load(ctx, sc.pool, out.Schema)
	if errors.Is(err, schemadiff.ErrSchemaNotFound) {
		httpapi.Invalid(w, "Schema tidak ditemukan.", map[string]string{"schema": "Schema tidak ditemukan."})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out.CurrentDDL = schemadiff.Dump(cur)
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in previewRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, err.Error(), nil)
		return
	}
	schema := strings.TrimSpace(in.Schema)
	if schema == "" {
		httpapi.Invalid(w, "Pilih schema untuk dibandingkan.", map[string]string{"schema": "Pilih schema untuk dibandingkan."})
		return
	}
	cur, err := schemadiff.Load(ctx, sc.pool, schema)
	if errors.Is(err, schemadiff.ErrSchemaNotFound) {
		httpapi.Invalid(w, "Schema tidak ditemukan.", map[string]string{"schema": "Schema tidak ditemukan."})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	desired, err := migration.ValidateSQL(in.DesiredDDL)
	if err != nil {
		httpapi.Invalid(w, err.Error(), map[string]string{"desired_ddl": err.Error()})
		return
	}
	want, err := schemadiff.LoadDDL(ctx, sc.pool, schema, desired)
	var ddlErr *schemadiff.DDLError
	switch {
	case errors.As(err, &ddlErr):
		httpapi.Invalid(w, ddlErr.Error(), map[string]string{"desired_ddl": ddlErr.Error()})
		return
	case errors.Is(err, schemadiff.ErrSchemaNotFound), errors.Is(err, schemadiff.ErrEmptyDDL):
		httpapi.Invalid(w, err.Error(), map[string]string{"desired_ddl": err.Error()})
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}
	res := schemadiff.Diff(cur, want, schemadiff.Options{Schema: schema, IncludeDestructive: in.IncludeDestructive, Comment: "Schema diff untuk schema " + schema + "."})
	out := resultDTO{Schema: schema, Title: "Schema diff " + schema + " " + time.Now().Format("02 Jan 2006 15:04"), Changes: make([]changeDTO, 0, len(res.Changes)), Warnings: res.Warnings, SQL: res.SQL, Statements: res.Statements, Skipped: res.Skipped, TooLarge: len(res.SQL) > migration.MaxSQLBytes}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	for _, c := range res.Changes {
		out.Changes = append(out.Changes, changeDTO{Op: string(c.Op), OpLabel: c.Op.Label(), Kind: c.Kind, Object: c.Object, Detail: c.Detail, Destructive: c.Destructive})
	}
	out.CanCreate = out.Statements > 0 && !out.TooLarge
	h.Logger.Info("schema diff previewed", "user_id", sc.p.UserID, "db_id", sc.conn.ID, "changes", len(out.Changes), "statements", out.Statements)
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, authz.ErrForbidden) {
		httpapi.Forbidden(w)
		return
	}
	if errors.Is(err, dbconn.ErrNotFound) {
		httpapi.NotFound(w)
		return
	}
	if ce, ok := dbconn.AsConnError(dbconn.Wrap(err)); ok {
		h.Logger.Warn("target database unreachable", "event", "schema_diff.target_unreachable", "kind", ce.Kind.String())
		httpapi.WriteError(w, http.StatusServiceUnavailable, "target_unavailable", ce.Kind.UserMessage(), nil)
		return
	}
	h.Logger.Error("request failed", "method", r.Method, "path", httpapi.LogPath(r), "err", err)
	httpapi.Internal(w)
}

func preferredSchema(schemas []string) string {
	for _, s := range schemas {
		if s == "public" {
			return s
		}
	}
	if len(schemas) > 0 {
		return schemas[0]
	}
	return ""
}
