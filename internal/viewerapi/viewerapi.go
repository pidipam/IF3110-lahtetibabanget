package viewerapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/explorer"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
)

type Handler struct {
	Conns   *dbconn.Store
	Manager *dbconn.Manager
	Authz   *authz.Authorizer
	Logger  *slog.Logger
}

type databaseDTO struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Engine string `json:"engine"`
	Level  string `json:"level"`
}

type columnDTO struct {
	Position   int    `json:"position"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	HasDefault bool   `json:"has_default"`
	Default    string `json:"default,omitempty"`
}

type constraintDTO struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Definition string   `json:"definition"`
	Columns    []string `json:"columns"`
}

type indexDTO struct {
	Name       string `json:"name"`
	Unique     bool   `json:"unique"`
	Primary    bool   `json:"primary"`
	Definition string `json:"definition"`
}

type tableMetaDTO struct {
	Schema      string          `json:"schema"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	KindLabel   string          `json:"kind_label"`
	Columns     []columnDTO     `json:"columns"`
	Constraints []constraintDTO `json:"constraints"`
	Indexes     []indexDTO      `json:"indexes"`
}

type schemaDTO struct {
	Name   string         `json:"name"`
	Tables []tableMetaDTO `json:"tables"`
}

type databaseDetailDTO struct {
	ID      int64       `json:"id"`
	Name    string      `json:"name"`
	Engine  string      `json:"engine"`
	Level   string      `json:"level"`
	CanEdit bool        `json:"can_edit"`
	Schemas []schemaDTO `json:"schemas"`
}

type cellDTO struct {
	Text      string `json:"text"`
	Null      bool   `json:"null"`
	Truncated bool   `json:"truncated"`
}

type paginationDTO struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	From       int64 `json:"from"`
	To         int64 `json:"to"`
	HasPrev    bool  `json:"has_prev"`
	HasNext    bool  `json:"has_next"`
	PrevPage   int   `json:"prev_page,omitempty"`
	NextPage   int   `json:"next_page,omitempty"`
}

type tablePageDTO struct {
	DatabaseID int64         `json:"database_id"`
	Schema     string        `json:"schema"`
	Name       string        `json:"name"`
	Kind       string        `json:"kind"`
	Columns    []string      `json:"columns"`
	Rows       [][]cellDTO   `json:"rows"`
	Pagination paginationDTO `json:"pagination"`
	Order      string        `json:"order"`
}

type pageFetcher func(ctx context.Context, cat *explorer.Catalog, schema, table string, page, size int) (*explorer.DataPage, error)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireLogin func(http.Handler) http.Handler) {
	handle := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, requireLogin(fn)) }
	handle("GET /api/databases", h.list)
	handle("GET /api/databases/{id}", h.detail)
	handle("GET /api/databases/{id}/tables/{schema}/{table}", h.table)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Logger.Error("request failed", "method", r.Method, "path", httpapi.LogPath(r), "error", err.Error())
	httpapi.Internal(w)
}

func (h *Handler) authorizeDB(w http.ResponseWriter, r *http.Request) (*dbconn.Connection, authz.Principal, bool) {
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return nil, authz.Principal{}, false
	}
	p, _ := auth.CurrentPrincipal(r.Context())
	if err := h.Authz.Can(r.Context(), p, id, authz.ViewDatabase); errors.Is(err, authz.ErrForbidden) {
		httpapi.Forbidden(w)
		return nil, p, false
	} else if err != nil {
		h.fail(w, r, err)
		return nil, p, false
	}
	c, err := h.Conns.Get(r.Context(), id)
	if errors.Is(err, dbconn.ErrNotFound) {
		httpapi.NotFound(w)
		return nil, p, false
	}
	if err != nil {
		h.fail(w, r, err)
		return nil, p, false
	}
	return c, p, true
}

func (h *Handler) dbFailure(w http.ResponseWriter, r *http.Request, c *dbconn.Connection, err error) {
	if errors.Is(err, dbconn.ErrNotFound) {
		httpapi.NotFound(w)
		return
	}
	ce, ok := dbconn.AsConnError(err)
	if !ok {
		h.fail(w, r, err)
		return
	}
	h.Logger.Warn("target database unreachable", "event", "viewer.target_unreachable", "connection_id", c.ID, "kind", ce.Kind.String())
	httpapi.WriteError(w, http.StatusServiceUnavailable, "target_unavailable", ce.Kind.UserMessage(), nil)
}

func (h *Handler) openCatalog(w http.ResponseWriter, r *http.Request, c *dbconn.Connection) (pageFetcher, *explorer.Catalog, bool) {
	if dbconn.IsMySQLFamily(c.Engine) {
		db, err := h.Manager.GetSQL(r.Context(), c.ID)
		if err != nil {
			h.dbFailure(w, r, c, err)
			return nil, nil, false
		}
		cat, err := explorer.IntrospectSQL(r.Context(), db)
		if err != nil {
			h.dbFailure(w, r, c, err)
			return nil, nil, false
		}
		fetch := func(ctx context.Context, cat *explorer.Catalog, schema, table string, page, size int) (*explorer.DataPage, error) {
			return explorer.FetchPageSQL(ctx, db, cat, schema, table, page, size)
		}
		return fetch, cat, true
	}
	pool, err := h.Manager.Get(r.Context(), c.ID)
	if err != nil {
		h.dbFailure(w, r, c, err)
		return nil, nil, false
	}
	cat, err := explorer.Introspect(r.Context(), pool)
	if err != nil {
		h.dbFailure(w, r, c, err)
		return nil, nil, false
	}
	fetch := func(ctx context.Context, cat *explorer.Catalog, schema, table string, page, size int) (*explorer.DataPage, error) {
		return explorer.FetchPage(ctx, pool, cat, schema, table, page, size)
	}
	return fetch, cat, true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, ok := httpapi.ParsePage(w, r)
	if !ok {
		return
	}
	principal, _ := auth.CurrentPrincipal(r.Context())
	levels, err := h.Authz.AccessibleDatabases(r.Context(), principal)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	ids := make([]int64, 0, len(levels))
	for id := range levels {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	conns, err := h.Conns.ListByIDs(r.Context(), ids)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]databaseDTO, 0, len(conns))
	for _, c := range conns {
		items = append(items, databaseDTO{ID: c.ID, Name: c.Name, Engine: c.Engine, Level: levels[c.ID].String()})
	}
	total := len(items)
	start := p.Offset()
	end := start + p.PageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	httpapi.WriteJSON(w, http.StatusOK, httpapi.NewPage(items[start:end], p, total))
}

func toTableMeta(t *explorer.Table) tableMetaDTO {
	out := tableMetaDTO{Schema: t.Schema, Name: t.Name, Kind: string(t.Kind), KindLabel: t.Kind.Label(), Columns: make([]columnDTO, 0, len(t.Columns)), Constraints: make([]constraintDTO, 0, len(t.Constraints)), Indexes: make([]indexDTO, 0, len(t.Indexes))}
	for _, c := range t.Columns {
		out.Columns = append(out.Columns, columnDTO{Position: c.Position, Name: c.Name, Type: c.Type, Nullable: c.Nullable, HasDefault: c.HasDefault, Default: c.Default})
	}
	for _, c := range t.Constraints {
		cols := c.Columns
		if cols == nil {
			cols = []string{}
		}
		out.Constraints = append(out.Constraints, constraintDTO{Name: c.Name, Type: string(c.Type), Definition: c.Definition, Columns: cols})
	}
	for _, i := range t.Indexes {
		out.Indexes = append(out.Indexes, indexDTO{Name: i.Name, Unique: i.Unique, Primary: i.Primary, Definition: i.Definition})
	}
	return out
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.authorizeDB(w, r)
	if !ok {
		return
	}
	level, err := h.Authz.Level(r.Context(), p, c.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	_, cat, ok := h.openCatalog(w, r, c)
	if !ok {
		return
	}
	schemas := make([]schemaDTO, 0, len(cat.Schemas))
	for _, s := range cat.Schemas {
		sd := schemaDTO{Name: s.Name, Tables: make([]tableMetaDTO, 0, len(s.Tables))}
		for _, t := range s.Tables {
			sd.Tables = append(sd.Tables, toTableMeta(t))
		}
		schemas = append(schemas, sd)
	}
	httpapi.WriteJSON(w, http.StatusOK, databaseDetailDTO{ID: c.ID, Name: c.Name, Engine: c.Engine, Level: level.String(), CanEdit: authz.Allows(level, authz.EditMigration), Schemas: schemas})
}

func toPagination(p explorer.Pagination) paginationDTO {
	out := paginationDTO{Page: p.Page, PageSize: p.PageSize, Total: p.Total, TotalPages: p.TotalPages, From: p.From, To: p.To, HasPrev: p.HasPrev, HasNext: p.HasNext, PrevPage: p.PrevPage, NextPage: p.NextPage}
	return out
}

func (h *Handler) table(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.authorizeDB(w, r)
	if !ok {
		return
	}
	p, ok := httpapi.ParsePage(w, r)
	if !ok {
		return
	}
	fetch, cat, ok := h.openCatalog(w, r, c)
	if !ok {
		return
	}
	dp, err := fetch(r.Context(), cat, r.PathValue("schema"), r.PathValue("table"), p.Page, p.PageSize)
	if errors.Is(err, explorer.ErrTableNotFound) {
		httpapi.NotFound(w)
		return
	}
	if err != nil {
		h.dbFailure(w, r, c, err)
		return
	}
	rows := make([][]cellDTO, 0, len(dp.Rows))
	for _, row := range dp.Rows {
		cells := make([]cellDTO, 0, len(row))
		for _, cell := range row {
			cells = append(cells, cellDTO{Text: cell.Text, Null: cell.Null, Truncated: cell.Truncated})
		}
		rows = append(rows, cells)
	}
	cols := dp.Columns
	if cols == nil {
		cols = []string{}
	}
	httpapi.WriteJSON(w, http.StatusOK, tablePageDTO{DatabaseID: c.ID, Schema: dp.Table.Schema, Name: dp.Table.Name, Kind: string(dp.Table.Kind), Columns: cols, Rows: rows, Pagination: toPagination(dp.Pagination), Order: string(dp.Ordering)})
}
