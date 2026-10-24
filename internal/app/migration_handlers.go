package app

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
)

func init() {
	notices["migration_saved"] = "Draft migration berhasil disimpan."
	notices["migration_updated"] = "Draft migration berhasil diperbarui."
	notices["migration_success"] = "Migration berhasil dijalankan dan di-commit."
}

func (a *App) migrationRoutes() {
	if a.Migrations == nil {
		return
	}
	login := func(h http.HandlerFunc) http.Handler { return a.Auth.RequireLogin(h) }
	a.mux.Handle("GET /databases/{dbID}/migrations", login(a.migrationList))
	a.mux.Handle("GET /databases/{dbID}/migrations/new", login(a.migrationNew))
	a.mux.Handle("POST /databases/{dbID}/migrations", login(a.migrationCreate))
	a.mux.Handle("GET /databases/{dbID}/migrations/{id}", login(a.migrationDetail))
	a.mux.Handle("GET /databases/{dbID}/migrations/{id}/edit", login(a.migrationEdit))
	a.mux.Handle("POST /databases/{dbID}/migrations/{id}", login(a.migrationUpdate))
	a.mux.Handle("GET /databases/{dbID}/migrations/{id}/review", login(a.migrationReview))
	a.mux.Handle("POST /databases/{dbID}/migrations/{id}/execute", login(a.migrationExecute))
}

type migScope struct {
	P    authz.Principal
	DBID int64
	Name string
}

// migScope memastikan user login, dbID valid, dan user boleh melihat database tersebut.
func (a *App) migScope(w http.ResponseWriter, r *http.Request) (migScope, bool) {
	p, ok := auth.CurrentPrincipal(r.Context())
	if !ok {
		a.unauthorized(w, r, false)
		return migScope{}, false
	}
	dbID, ok := pathID(r, "dbID")
	if !ok {
		a.notFound(w, r)
		return migScope{}, false
	}
	name, err := a.Migrations.TargetName(r.Context(), p, dbID)
	if err != nil {
		a.migrationError(w, r, err)
		return migScope{}, false
	}
	return migScope{P: p, DBID: dbID, Name: name}, true
}

func (a *App) migrationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, authz.ErrForbidden):
		a.forbidden(w, r)
	case errors.Is(err, migration.ErrNotFound), errors.Is(err, migration.ErrTargetNotFound):
		a.notFound(w, r)
	case errors.Is(err, migration.ErrBusy):
		a.RenderError(w, r, http.StatusConflict, "Sedang Sibuk", err.Error(), false)
	case errors.Is(err, migration.ErrTargetUnavailable):
		a.Logger.Error("target database tidak dapat dihubungi", "path", r.URL.Path, "err", err)
		a.RenderError(w, r, http.StatusBadGateway, "Koneksi Database Gagal", "Database target tidak dapat dihubungi. Periksa koneksi lalu coba lagi.", false)
	case errors.Is(err, migration.ErrNotDraft):
		a.RenderError(w, r, http.StatusConflict, "Tidak Dapat Diubah", err.Error(), false)
	case errors.Is(err, migration.ErrChanged):
		a.RenderError(w, r, http.StatusConflict, "Tidak Dapat Dijalankan", err.Error(), false)
	default:
		a.fail(w, r, err)
	}
}

func isValidationError(err error) bool {
	for _, e := range []error{migration.ErrEmptyTitle, migration.ErrTitleTooLong, migration.ErrEmptySQL,
		migration.ErrSQLTooLong, migration.ErrInvalidSQL, migration.ErrTxControl} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func migBase(dbID int64) string { return fmt.Sprintf("/databases/%d/migrations", dbID) }

func (a *App) renderMigrationForm(w http.ResponseWriter, r *http.Request, status int, sc migScope, heading, action, title, sql, msg string) {
	v := a.View(r, heading, map[string]any{
		"DBID": sc.DBID, "DBName": sc.Name, "Heading": heading,
		"Action": action, "Back": migBase(sc.DBID), "Title": title, "SQL": sql,
	})
	v.Error = msg
	a.Render.Render(w, status, "migration_edit", v)
}

func (a *App) migrationList(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	list, err := a.Migrations.List(r.Context(), sc.P, sc.DBID)
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	a.Render.Render(w, http.StatusOK, "migration_history", a.View(r, "Riwayat Migration", map[string]any{
		"DBID": sc.DBID, "DBName": sc.Name, "Migrations": list,
		"CanEdit": a.Migrations.CanEdit(r.Context(), sc.P, sc.DBID),
	}))
}

func (a *App) migrationDetail(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return
	}
	m, err := a.Migrations.Get(r.Context(), sc.P, sc.DBID, id)
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	a.Render.Render(w, http.StatusOK, "migration_detail", a.View(r, "Detail Migration", map[string]any{
		"DBID": sc.DBID, "DBName": sc.Name, "M": m, "Back": migBase(sc.DBID),
		"CanEdit": a.Migrations.CanEdit(r.Context(), sc.P, sc.DBID),
	}))
}

func (a *App) migrationNew(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	if !a.Migrations.CanEdit(r.Context(), sc.P, sc.DBID) {
		a.forbidden(w, r)
		return
	}
	a.renderMigrationForm(w, r, http.StatusOK, sc, "Buat Migration", migBase(sc.DBID), "", "", "")
}

func (a *App) migrationCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	title, sql := r.PostFormValue("title"), r.PostFormValue("sql")
	m, err := a.Migrations.Create(r.Context(), sc.P, sc.DBID, title, sql)
	if isValidationError(err) {
		a.renderMigrationForm(w, r, http.StatusUnprocessableEntity, sc, "Buat Migration", migBase(sc.DBID), title, sql, err.Error())
		return
	}
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	a.Logger.Info("migration draft created", "user_id", sc.P.UserID, "db_id", sc.DBID, "migration_id", m.ID)
	http.Redirect(w, r, fmt.Sprintf("%s/%d?ok=migration_saved", migBase(sc.DBID), m.ID), http.StatusSeeOther)
}

func (a *App) migrationEdit(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return
	}
	m, err := a.Migrations.Get(r.Context(), sc.P, sc.DBID, id)
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	if !a.Migrations.CanEdit(r.Context(), sc.P, sc.DBID) {
		a.forbidden(w, r)
		return
	}
	if !m.Status.Editable() {
		a.migrationError(w, r, migration.ErrNotDraft)
		return
	}
	a.renderMigrationForm(w, r, http.StatusOK, sc, "Edit Draft Migration", fmt.Sprintf("%s/%d", migBase(sc.DBID), m.ID), m.Title, m.SQL, "")
}

func (a *App) migrationUpdate(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return
	}
	title, sql := r.PostFormValue("title"), r.PostFormValue("sql")
	err := a.Migrations.Update(r.Context(), sc.P, sc.DBID, id, title, sql)
	if isValidationError(err) {
		a.renderMigrationForm(w, r, http.StatusUnprocessableEntity, sc, "Edit Draft Migration", fmt.Sprintf("%s/%d", migBase(sc.DBID), id), title, sql, err.Error())
		return
	}
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	a.Logger.Info("migration draft updated", "user_id", sc.P.UserID, "db_id", sc.DBID, "migration_id", id)
	http.Redirect(w, r, fmt.Sprintf("%s/%d?ok=migration_updated", migBase(sc.DBID), id), http.StatusSeeOther)
}

func (a *App) renderMigrationReview(w http.ResponseWriter, r *http.Request, status int, sc migScope, rv *migration.Review, msg string) {
	v := a.View(r, "Tinjau Migration", map[string]any{
		"DBID": sc.DBID, "DBName": sc.Name, "M": rv.M,
		"NextVersion": rv.NextVersion, "Checksum": rv.Checksum, "Back": migBase(sc.DBID),
	})
	v.Error = msg
	a.Render.Render(w, status, "migration_review", v)
}

func (a *App) migrationReview(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return
	}
	rv, err := a.Migrations.Review(r.Context(), sc.P, sc.DBID, id)
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	a.renderMigrationReview(w, r, http.StatusOK, sc, rv, "")
}

func (a *App) migrationExecute(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.migScope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return
	}
	if r.PostFormValue("confirm") == "" {
		rv, err := a.Migrations.Review(r.Context(), sc.P, sc.DBID, id)
		if err != nil {
			a.migrationError(w, r, err)
			return
		}
		a.renderMigrationReview(w, r, http.StatusUnprocessableEntity, sc, rv, "Centang konfirmasi sebelum menjalankan migration.")
		return
	}
	m, err := a.Migrations.ExecuteReviewed(r.Context(), sc.P, sc.DBID, id, r.PostFormValue("checksum"))
	if err != nil {
		a.migrationError(w, r, err)
		return
	}
	a.Logger.Info("migration executed", "user_id", sc.P.UserID, "db_id", sc.DBID, "migration_id", m.ID, "status", string(m.Status))
	target := fmt.Sprintf("%s/%d", migBase(sc.DBID), m.ID)
	if m.Status == migration.StatusSuccess {
		target += "?ok=migration_success"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
