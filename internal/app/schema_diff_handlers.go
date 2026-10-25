package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/schemadiff"
)

func init() {
	notices["schema_diff_saved"] = "Draft migration dari schema diff berhasil dibuat."
}

type schemaDiffScope struct {
	P    authz.Principal
	DBID int64
	Conn *dbconn.Connection
	Pool *pgxpool.Pool
}

func schemaDiffBase(dbID int64) string {
	return fmt.Sprintf("/databases/%d/schema-diff", dbID)
}

func (a *App) schemaDiffRoutes() {
	if a.Migrations == nil {
		return
	}
	login := func(h http.HandlerFunc) http.Handler { return a.Auth.RequireLogin(h) }
	a.mux.Handle("GET /databases/{dbID}/schema-diff", login(a.schemaDiffPage))
	a.mux.Handle("POST /databases/{dbID}/schema-diff", login(a.schemaDiffPreview))
	a.mux.Handle("POST /databases/{dbID}/schema-diff/migration", login(a.schemaDiffCreate))
}

func (a *App) schemaDiffScope(w http.ResponseWriter, r *http.Request) (schemaDiffScope, bool) {
	ctx := r.Context()
	p, ok := auth.CurrentPrincipal(ctx)
	if !ok {
		a.unauthorized(w, r, false)
		return schemaDiffScope{}, false
	}
	dbID, ok := pathID(r, "dbID")
	if !ok {
		a.notFound(w, r)
		return schemaDiffScope{}, false
	}
	if err := a.Authz.Can(ctx, p, dbID, authz.EditMigration); err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			a.forbidden(w, r)
		} else {
			a.fail(w, r, err)
		}
		return schemaDiffScope{}, false
	}
	c, err := a.Conns.Get(ctx, dbID)
	if errors.Is(err, dbconn.ErrNotFound) {
		a.notFound(w, r)
		return schemaDiffScope{}, false
	}
	if err != nil {
		a.fail(w, r, err)
		return schemaDiffScope{}, false
	}
	pool, err := a.Manager.Get(ctx, c.ID)
	if err != nil {
		a.schemaDiffTargetError(w, r, c, err)
		return schemaDiffScope{}, false
	}
	return schemaDiffScope{P: p, DBID: dbID, Conn: c, Pool: pool}, true
}

func (a *App) schemaDiffTargetError(w http.ResponseWriter, r *http.Request, c *dbconn.Connection, err error) {
	if errors.Is(err, dbconn.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if _, ok := dbconn.AsConnError(err); ok {
		a.dbFailure(w, r, c, err)
		return
	}
	a.fail(w, r, err)
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

func (a *App) renderSchemaDiffForm(w http.ResponseWriter, r *http.Request, status int, sc schemaDiffScope, schemas []string, schema, current, desired string, include bool, msg string) {
	data := map[string]any{
		"Conn": sc.Conn, "DBID": sc.DBID, "DBName": sc.Conn.Name,
		"Schemas": schemas, "Schema": schema, "CurrentDDL": current,
		"Desired": desired, "IncludeDestructive": include,
		"Action": schemaDiffBase(sc.DBID), "Back": dbPath(sc.DBID),
	}
	v := a.View(r, "Schema Diff", data)
	v.Error = msg
	a.Render.Render(w, status, "schema_diff_form", v)
}

func (a *App) renderSchemaDiffResult(w http.ResponseWriter, r *http.Request, status int, sc schemaDiffScope, schema string, res *schemadiff.Result, title, sql, msg string) {
	var changes []schemadiff.Change
	var warnings []string
	var statements, skipped int
	if res != nil {
		changes, warnings = res.Changes, res.Warnings
		statements, skipped = res.Statements, res.Skipped
		sql = res.SQL
	}
	canCreate := strings.TrimSpace(sql) != "" && len(sql) <= migration.MaxSQLBytes && statements > 0
	if res == nil {
		canCreate = strings.TrimSpace(sql) != "" && len(sql) <= migration.MaxSQLBytes
	}
	back := schemaDiffBase(sc.DBID)
	if schema != "" {
		back += "?schema=" + url.QueryEscape(schema)
	}
	data := map[string]any{
		"Conn": sc.Conn, "DBID": sc.DBID, "DBName": sc.Conn.Name, "Schema": schema,
		"Changes": changes, "Warnings": warnings, "Title": title, "SQL": sql,
		"Statements": statements, "Skipped": skipped, "CanCreate": canCreate,
		"CreateAction": schemaDiffBase(sc.DBID) + "/migration", "Back": back,
	}
	v := a.View(r, "Hasil Schema Diff", data)
	v.Error = msg
	a.Render.Render(w, status, "schema_diff_result", v)
}

func (a *App) schemaDiffPage(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.schemaDiffScope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	schemas, err := schemadiff.Schemas(ctx, sc.Pool)
	if err != nil {
		a.schemaDiffTargetError(w, r, sc.Conn, err)
		return
	}
	schema := strings.TrimSpace(r.URL.Query().Get("schema"))
	if schema == "" {
		schema = preferredSchema(schemas)
	}
	if schema == "" {
		a.renderSchemaDiffForm(w, r, http.StatusOK, sc, nil, "", "", "", false, "Database ini tidak punya schema yang dapat dibandingkan.")
		return
	}
	cur, err := schemadiff.Load(ctx, sc.Pool, schema)
	if errors.Is(err, schemadiff.ErrSchemaNotFound) {
		a.renderSchemaDiffForm(w, r, http.StatusUnprocessableEntity, sc, schemas, schema, "", "", false, "Schema tidak ditemukan.")
		return
	}
	if err != nil {
		a.schemaDiffTargetError(w, r, sc.Conn, err)
		return
	}
	current := schemadiff.Dump(cur)
	a.renderSchemaDiffForm(w, r, http.StatusOK, sc, schemas, schema, current, current, false, "")
}

func (a *App) schemaDiffPreview(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.schemaDiffScope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	schema := strings.TrimSpace(r.PostFormValue("schema"))
	desired := r.PostFormValue("desired_ddl")
	include := r.PostFormValue("include_destructive") != ""

	schemas, err := schemadiff.Schemas(ctx, sc.Pool)
	if err != nil {
		a.schemaDiffTargetError(w, r, sc.Conn, err)
		return
	}
	current := ""
	fail := func(msg string) {
		a.renderSchemaDiffForm(w, r, http.StatusUnprocessableEntity, sc, schemas, schema, current, desired, include, msg)
	}
	if schema == "" {
		fail("Pilih schema untuk dibandingkan.")
		return
	}
	cur, err := schemadiff.Load(ctx, sc.Pool, schema)
	if errors.Is(err, schemadiff.ErrSchemaNotFound) {
		fail("Schema tidak ditemukan.")
		return
	}
	if err != nil {
		a.schemaDiffTargetError(w, r, sc.Conn, err)
		return
	}
	current = schemadiff.Dump(cur)
	desired, err = migration.ValidateSQL(desired)
	if err != nil {
		fail(err.Error())
		return
	}
	want, err := schemadiff.LoadDDL(ctx, sc.Pool, schema, desired)
	var ddlErr *schemadiff.DDLError
	switch {
	case errors.As(err, &ddlErr):
		fail(ddlErr.Error())
		return
	case errors.Is(err, schemadiff.ErrSchemaNotFound), errors.Is(err, schemadiff.ErrEmptyDDL):
		fail(err.Error())
		return
	case err != nil:
		a.schemaDiffTargetError(w, r, sc.Conn, err)
		return
	}
	res := schemadiff.Diff(cur, want, schemadiff.Options{
		Schema:             schema,
		IncludeDestructive: include,
		Comment:            "Schema diff untuk schema " + schema + ".",
	})
	title := "Schema diff " + schema + " " + time.Now().Format("02 Jan 2006 15:04")
	switch {
	case res.Empty():
		a.renderSchemaDiffResult(w, r, http.StatusOK, sc, schema, res, title, res.SQL, "Tidak ada perbedaan antara schema saat ini dan DDL yang diinginkan.")
	case res.Statements == 0:
		a.renderSchemaDiffResult(w, r, http.StatusOK, sc, schema, res, title, res.SQL, "Tidak ada statement aktif. Centang \"Sertakan perubahan destruktif\" apabila ingin menghapus objek.")
	case len(res.SQL) > migration.MaxSQLBytes:
		a.renderSchemaDiffResult(w, r, http.StatusUnprocessableEntity, sc, schema, res, title, res.SQL, "Hasil SQL melebihi 100 KB.")
	default:
		a.renderSchemaDiffResult(w, r, http.StatusOK, sc, schema, res, title, res.SQL, "")
	}
}

func (a *App) schemaDiffCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.schemaDiffScope(w, r)
	if !ok {
		return
	}
	schema := strings.TrimSpace(r.PostFormValue("schema"))
	title := r.PostFormValue("title")
	sql := r.PostFormValue("sql")
	m, err := a.Migrations.Create(r.Context(), sc.P, sc.DBID, title, sql)
	if isValidationError(err) {
		a.renderSchemaDiffResult(w, r, http.StatusUnprocessableEntity, sc, schema, nil, title, sql, err.Error())
		return
	}
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	a.Logger.Info("schema diff draft created", "user_id", sc.P.UserID, "db_id", sc.DBID, "migration_id", m.ID)
	http.Redirect(w, r, fmt.Sprintf("%s/%d?ok=schema_diff_saved", migBase(sc.DBID), m.ID), http.StatusSeeOther)
}
