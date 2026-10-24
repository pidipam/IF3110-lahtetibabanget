package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

const msgConnInUse = "Koneksi tidak dapat dihapus karena sudah memiliki riwayat migration."

type connForm struct {
	Name, Host, Port, DBName, Username, SSLMode string
}

func connFormOf(c *dbconn.Connection) connForm {
	return connForm{Name: c.Name, Host: c.Host, Port: strconv.Itoa(c.Port), DBName: c.DBName, Username: c.Username, SSLMode: c.SSLMode}
}

func readConnForm(r *http.Request) (connForm, dbconn.Input) {
	port := strings.TrimSpace(r.PostFormValue("port"))
	n, err := strconv.Atoi(port)
	if err != nil {
		n = 0
	}
	in := dbconn.Input{Name: r.PostFormValue("name"), Host: r.PostFormValue("host"), Port: n, DBName: r.PostFormValue("db_name"), Username: r.PostFormValue("username"), Password: r.PostFormValue("password"), SSLMode: r.PostFormValue("sslmode")}
	in.Normalize()
	return connForm{Name: in.Name, Host: in.Host, Port: port, DBName: in.DBName, Username: in.Username, SSLMode: in.SSLMode}, in
}

func (a *App) loadConn(w http.ResponseWriter, r *http.Request) (*dbconn.Connection, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return nil, false
	}
	c, err := a.Conns.Get(r.Context(), id)
	if errors.Is(err, dbconn.ErrNotFound) {
		a.notFound(w, r)
		return nil, false
	}
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	return c, true
}

func (a *App) adminConnections(w http.ResponseWriter, r *http.Request) {
	list, err := a.Conns.List(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Render.Render(w, http.StatusOK, "admin_connections", a.View(r, "Kelola Koneksi", map[string]any{"Conns": list}))
}

func (a *App) renderConnForm(w http.ResponseWriter, r *http.Request, status int, conn *dbconn.Connection, f connForm, errMsg, okMsg string) {
	data := map[string]any{"Conn": conn, "Form": f, "SSLModes": dbconn.SSLModes()}
	title := "Tambah Koneksi"
	if conn != nil {
		grants, err := a.GrantStore.ListByConnection(r.Context(), conn.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		data["Grants"] = grants
		title = "Edit Koneksi"
	}
	v := a.View(r, title, data)
	v.Error = errMsg
	if okMsg != "" {
		v.Notice = okMsg
	}
	a.Render.Render(w, status, "admin_connection_form", v)
}

func (a *App) adminConnectionNew(w http.ResponseWriter, r *http.Request) {
	a.renderConnForm(w, r, http.StatusOK, nil, connForm{Port: "5432", SSLMode: "prefer"}, "", "")
}

func connErrMsg(err error) (string, bool) {
	var verr *dbconn.ValidationError
	switch {
	case errors.As(err, &verr):
		return verr.Msg, true
	case errors.Is(err, dbconn.ErrDuplicate):
		return "Nama koneksi sudah dipakai.", true
	}
	return "", false
}

func (a *App) adminConnectionCreate(w http.ResponseWriter, r *http.Request) {
	f, in := readConnForm(r)
	c, err := a.Conns.Create(r.Context(), in)
	if msg, ok := connErrMsg(err); ok {
		a.renderConnForm(w, r, http.StatusUnprocessableEntity, nil, f, msg, "")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Logger.Info("admin created connection", "admin_id", a.actorID(r), "connection_id", c.ID)
	http.Redirect(w, r, "/admin/connections?ok=connection_created", http.StatusSeeOther)
}

func (a *App) adminConnectionEdit(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadConn(w, r)
	if !ok {
		return
	}
	a.renderConnForm(w, r, http.StatusOK, c, connFormOf(c), "", "")
}

func (a *App) adminConnectionUpdate(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadConn(w, r)
	if !ok {
		return
	}
	f, in := readConnForm(r)
	_, err := a.Conns.Update(r.Context(), c.ID, in)
	if msg, ok := connErrMsg(err); ok {
		a.renderConnForm(w, r, http.StatusUnprocessableEntity, c, f, msg, "")
		return
	}
	if errors.Is(err, dbconn.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Manager.Invalidate(c.ID)
	a.Logger.Info("admin updated connection", "admin_id", a.actorID(r), "connection_id", c.ID)
	http.Redirect(w, r, fmt.Sprintf("/admin/connections/%d?ok=connection_updated", c.ID), http.StatusSeeOther)
}

func (a *App) adminConnectionDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, ok := a.loadConn(w, r)
	if !ok {
		return
	}
	grants, err := a.GrantStore.ListByConnection(ctx, c.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	err = a.Conns.Delete(ctx, c.ID)
	if errors.Is(err, dbconn.ErrInUse) {
		a.renderConnForm(w, r, http.StatusConflict, c, connFormOf(c), msgConnInUse, "")
		return
	}
	if errors.Is(err, dbconn.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Manager.Invalidate(c.ID)
	// Grant ikut terhapus (cascade)
	for _, g := range grants {
		if err := a.Sessions.RevokeGroup(ctx, g.GroupID); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.Logger.Info("admin deleted connection", "admin_id", a.actorID(r), "connection_id", c.ID)
	http.Redirect(w, r, "/admin/connections?ok=connection_deleted", http.StatusSeeOther)
}

func (a *App) adminConnectionTestNew(w http.ResponseWriter, r *http.Request) {
	a.testConnection(w, r, nil)
}

func (a *App) adminConnectionTestEdit(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadConn(w, r)
	if !ok {
		return
	}
	a.testConnection(w, r, c)
}

func (a *App) testConnection(w http.ResponseWriter, r *http.Request, c *dbconn.Connection) {
	ctx := r.Context()
	f, in := readConnForm(r)
	if err := in.Validate(c == nil); err != nil {
		a.renderConnForm(w, r, http.StatusUnprocessableEntity, c, f, err.Error(), "")
		return
	}
	var p dbconn.Params
	var connID int64
	if c == nil {
		p = dbconn.ParamsFromInput(in)
	} else {
		connID = c.ID
		var err error
		if p, err = a.Conns.ParamsForEdit(ctx, c.ID, in); errors.Is(err, dbconn.ErrNotFound) {
			a.notFound(w, r)
			return
		} else if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if err := a.Manager.Test(ctx, p); err != nil {
		ce, ok := dbconn.AsConnError(err)
		if !ok {
			a.fail(w, r, err)
			return
		}
		a.Logger.Info("admin tested connection", "admin_id", a.actorID(r), "connection_id", connID, "success", false, "kind", ce.Kind.String())
		a.renderConnForm(w, r, http.StatusOK, c, f, "Test koneksi gagal. "+ce.Kind.UserMessage(), "")
		return
	}
	a.Logger.Info("admin tested connection", "admin_id", a.actorID(r), "connection_id", connID, "success", true)
	a.renderConnForm(w, r, http.StatusOK, c, f, "", "Test koneksi berhasil.")
}

type accessRow struct {
	Conn  *dbconn.Connection
	Level string
}

func (a *App) renderGroupAccess(w http.ResponseWriter, r *http.Request, status int, g *users.Group, msg string) {
	ctx := r.Context()
	conns, err := a.Conns.List(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	grants, err := a.GrantStore.ListByGroup(ctx, g.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	cur := map[int64]authz.Level{}
	for _, gr := range grants {
		cur[gr.ConnID] = gr.Level
	}
	rows := make([]accessRow, 0, len(conns))
	for _, c := range conns {
		rows = append(rows, accessRow{Conn: c, Level: cur[c.ID].String()})
	}
	v := a.View(r, "Akses Database Grup", map[string]any{"Group": g, "Rows": rows})
	v.Error = msg
	a.Render.Render(w, status, "admin_group_access", v)
}

func (a *App) adminGroupAccess(w http.ResponseWriter, r *http.Request) {
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	a.renderGroupAccess(w, r, http.StatusOK, g, "")
}

func (a *App) adminGroupAccessUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	conns, err := a.Conns.List(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	grants, err := a.GrantStore.ListByGroup(ctx, g.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	cur := map[int64]authz.Level{}
	for _, gr := range grants {
		cur[gr.ConnID] = gr.Level
	}
	type change struct {
		connID int64
		level  authz.Level
	}
	var changes []change
	r.ParseForm()
	for _, c := range conns {
		raw, sent := r.PostForm["level_"+strconv.FormatInt(c.ID, 10)]
		if !sent || len(raw) == 0 {
			continue
		}
		want := authz.ParseLevel(raw[0])
		if raw[0] != "none" && want == authz.LevelNone {
			a.renderGroupAccess(w, r, http.StatusUnprocessableEntity, g, "Level akses tidak valid.")
			return
		}
		if want != cur[c.ID] {
			changes = append(changes, change{c.ID, want})
		}
	}
	for _, ch := range changes {
		if ch.level == authz.LevelNone {
			err = a.Grants.RemoveAccess(ctx, g.ID, ch.connID)
		} else {
			err = a.Grants.SetAccess(ctx, g.ID, ch.connID, ch.level)
		}
		if errors.Is(err, dbconn.ErrNotFound) {
			a.renderGroupAccess(w, r, http.StatusUnprocessableEntity, g, "Koneksi tidak ditemukan. Muat ulang halaman lalu coba lagi.")
			return
		}
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.Logger.Info("admin updated group access", "admin_id", a.actorID(r), "group_id", g.ID, "changes", len(changes))
	http.Redirect(w, r, fmt.Sprintf("/admin/groups/%d/access?ok=access_updated", g.ID), http.StatusSeeOther)
}
