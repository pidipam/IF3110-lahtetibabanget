package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/explorer"
)

type dbRow struct {
	Conn  *dbconn.Connection
	Level authz.Level
	Href  string
}

type tableRow struct {
	Name, Kind, Href string
}

type schemaRow struct {
	Name   string
	Tables []tableRow
}

func dbPath(id int64) string { return "/databases/" + strconv.FormatInt(id, 10) }

func tablePath(id int64, schema, table string) string {
	return dbPath(id) + "/tables/" + url.PathEscape(schema) + "/" + url.PathEscape(table)
}

func (a *App) databases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.CurrentPrincipal(ctx)
	levels, err := a.Authz.AccessibleDatabases(ctx, p)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ids := make([]int64, 0, len(levels))
	for id := range levels {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	conns, err := a.Conns.ListByIDs(ctx, ids)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rows := make([]dbRow, 0, len(conns))
	for _, c := range conns {
		rows = append(rows, dbRow{Conn: c, Level: levels[c.ID], Href: dbPath(c.ID)})
	}
	a.Render.Render(w, http.StatusOK, "databases", a.View(r, "Database", map[string]any{"Rows": rows}))
}

// Urutannya: format id (404), akses (403, termasuk id yang tidak ada), lalu keberadaan koneksi (404).
func (a *App) authorizeDB(w http.ResponseWriter, r *http.Request) (*dbconn.Connection, authz.Principal, bool) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return nil, authz.Principal{}, false
	}
	p, _ := auth.CurrentPrincipal(ctx)
	if err := a.Authz.Can(ctx, p, id, authz.ViewDatabase); errors.Is(err, authz.ErrForbidden) {
		a.forbidden(w, r)
		return nil, p, false
	} else if err != nil {
		a.fail(w, r, err)
		return nil, p, false
	}
	c, err := a.Conns.Get(ctx, id)
	if errors.Is(err, dbconn.ErrNotFound) {
		a.notFound(w, r)
		return nil, p, false
	}
	if err != nil {
		a.fail(w, r, err)
		return nil, p, false
	}
	return c, p, true
}

func (a *App) dbFailure(w http.ResponseWriter, r *http.Request, c *dbconn.Connection, err error) {
	if errors.Is(err, dbconn.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	ce, ok := dbconn.AsConnError(err)
	if !ok {
		a.fail(w, r, err)
		return
	}
	a.Logger.Warn("target database unreachable", "connection_id", c.ID, "kind", ce.Kind.String())
	v := a.View(r, "Database Tidak Dapat Dihubungi", map[string]any{"Conn": c, "Message": ce.Kind.UserMessage(), "Code": http.StatusServiceUnavailable, "Retry": r.URL.RequestURI()})
	a.Render.Render(w, http.StatusServiceUnavailable, "database_error", v)
}

type pageFetcher func(ctx context.Context, cat *explorer.Catalog, schema, table string, page, size int) (*explorer.DataPage, error)

func (a *App) openCatalog(w http.ResponseWriter, r *http.Request, c *dbconn.Connection) (pageFetcher, *explorer.Catalog, bool) {
	ctx := r.Context()
	if dbconn.IsMySQLFamily(c.Engine) {
		db, err := a.Manager.GetSQL(ctx, c.ID)
		if err != nil {
			a.dbFailure(w, r, c, err)
			return nil, nil, false
		}
		cat, err := explorer.IntrospectSQL(ctx, db)
		if err != nil {
			a.dbFailure(w, r, c, err)
			return nil, nil, false
		}
		fetch := func(ctx context.Context, cat *explorer.Catalog, schema, table string, page, size int) (*explorer.DataPage, error) {
			return explorer.FetchPageSQL(ctx, db, cat, schema, table, page, size)
		}
		return fetch, cat, true
	}
	pool, err := a.Manager.Get(ctx, c.ID)
	if err != nil {
		a.dbFailure(w, r, c, err)
		return nil, nil, false
	}
	cat, err := explorer.Introspect(ctx, pool)
	if err != nil {
		a.dbFailure(w, r, c, err)
		return nil, nil, false
	}
	fetch := func(ctx context.Context, cat *explorer.Catalog, schema, table string, page, size int) (*explorer.DataPage, error) {
		return explorer.FetchPage(ctx, pool, cat, schema, table, page, size)
	}
	return fetch, cat, true
}

func (a *App) databaseView(w http.ResponseWriter, r *http.Request) {
	c, p, ok := a.authorizeDB(w, r)
	if !ok {
		return
	}
	level, err := a.Authz.Level(r.Context(), p, c.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	_, cat, ok := a.openCatalog(w, r, c)
	if !ok {
		return
	}
	schemas := make([]schemaRow, 0, len(cat.Schemas))
	for _, s := range cat.Schemas {
		sr := schemaRow{Name: s.Name}
		for _, t := range s.Tables {
			sr.Tables = append(sr.Tables, tableRow{Name: t.Name, Kind: t.Kind.Label(), Href: tablePath(c.ID, t.Schema, t.Name)})
		}
		schemas = append(schemas, sr)
	}
	a.Render.Render(w, http.StatusOK, "database_detail", a.View(r, c.Name, map[string]any{
		"Conn": c, "Level": level, "Schemas": schemas,
		"MigrationHref":  dbPath(c.ID) + "/migrations",
		"CanEdit":        authz.Allows(level, authz.EditMigration),
		"SchemaDiffHref": dbPath(c.ID) + "/schema-diff",
	}))
}

func orderNote(o explorer.Ordering) string {
	switch o {
	case explorer.OrderPK:
		return "Baris diurutkan berdasarkan primary key."
	case explorer.OrderCTID:
		return "Tabel ini tidak punya primary key, baris diurutkan berdasarkan posisi fisik (ctid)."
	}
	return "Objek ini tidak punya urutan yang stabil, sehingga urutan baris dan isi tiap halaman bisa berubah."
}

func (a *App) databaseTable(w http.ResponseWriter, r *http.Request) {
	c, _, ok := a.authorizeDB(w, r)
	if !ok {
		return
	}
	fetch, cat, ok := a.openCatalog(w, r, c)
	if !ok {
		return
	}
	schema, table := r.PathValue("schema"), r.PathValue("table")
	page, size := explorer.ParsePageParams(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	dp, err := fetch(r.Context(), cat, schema, table, page, size)
	if errors.Is(err, explorer.ErrTableNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.dbFailure(w, r, c, err)
		return
	}
	base := tablePath(c.ID, dp.Table.Schema, dp.Table.Name)
	link := func(n int) string {
		return base + "?" + url.Values{"page": {strconv.Itoa(n)}, "page_size": {strconv.Itoa(dp.Pagination.PageSize)}}.Encode()
	}
	data := map[string]any{"Conn": c, "Page": dp, "Table": dp.Table, "OrderNote": orderNote(dp.Ordering), "DBHref": dbPath(c.ID), "Base": base, "PageSizes": []int{10, 25, 50, 100}}
	if dp.Pagination.HasPrev {
		data["PrevHref"] = link(dp.Pagination.PrevPage)
	}
	if dp.Pagination.HasNext {
		data["NextHref"] = link(dp.Pagination.NextPage)
	}
	a.Render.Render(w, http.StatusOK, "database_table", a.View(r, dp.Table.FullName(), data))
}
