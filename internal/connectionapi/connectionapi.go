package connectionapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
)

const msgConnInUse = "Koneksi tidak dapat dihapus karena sudah memiliki riwayat migration."

type sessionRevoker interface {
	RevokeGroup(context.Context, int64) error
}

type Handler struct {
	Conns    *dbconn.Store
	Manager  *dbconn.Manager
	Grants   *dbconn.GrantStore
	Sessions sessionRevoker
	Logger   *slog.Logger
}

type connectionRequest struct {
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"db_name"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
}

type ConnectionDTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Engine    string    `json:"engine"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	DBName    string    `json:"db_name"`
	Username  string    `json:"username"`
	SSLMode   string    `json:"sslmode"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type listResponse struct {
	Items []ConnectionDTO `json:"items"`
}

type testResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireAdmin func(http.Handler) http.Handler) {
	handle := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, requireAdmin(fn)) }
	handle("GET /api/admin/connections", h.list)
	handle("POST /api/admin/connections", h.create)
	handle("POST /api/admin/connections/test", h.testNew)
	handle("GET /api/admin/connections/{id}", h.get)
	handle("PATCH /api/admin/connections/{id}", h.update)
	handle("DELETE /api/admin/connections/{id}", h.delete)
	handle("POST /api/admin/connections/{id}/test", h.testSaved)
}

func toDTO(c *dbconn.Connection) ConnectionDTO {
	return ConnectionDTO{ID: c.ID, Name: c.Name, Engine: c.Engine, Host: c.Host, Port: c.Port, DBName: c.DBName, Username: c.Username, SSLMode: c.SSLMode, CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC()}
}

func (in connectionRequest) input() dbconn.Input {
	return dbconn.Input{Name: in.Name, Engine: in.Engine, Host: in.Host, Port: in.Port, DBName: in.DBName, Username: in.Username, Password: in.Password, SSLMode: in.SSLMode}
}

func fieldOf(msg string) string {
	switch {
	case strings.Contains(msg, "Nama koneksi"):
		return "name"
	case strings.Contains(msg, "Host"):
		return "host"
	case strings.Contains(msg, "Port"):
		return "port"
	case strings.Contains(msg, "Nama database"):
		return "db_name"
	case strings.Contains(msg, "Username"):
		return "username"
	case strings.Contains(msg, "Password"):
		return "password"
	case strings.Contains(msg, "SSL"):
		return "sslmode"
	case strings.Contains(msg, "Engine"):
		return "engine"
	}
	return "input"
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Logger.Error("request failed", "method", r.Method, "path", httpapi.LogPath(r), "error", err.Error())
	httpapi.Internal(w)
}

func (h *Handler) writeConnErr(w http.ResponseWriter, r *http.Request, err error) {
	var verr *dbconn.ValidationError
	switch {
	case errors.As(err, &verr):
		httpapi.Invalid(w, verr.Msg, map[string]string{fieldOf(verr.Msg): verr.Msg})
	case errors.Is(err, dbconn.ErrDuplicate):
		httpapi.WriteError(w, http.StatusConflict, "conflict", "Nama koneksi sudah dipakai.", map[string]string{"name": "sudah dipakai"})
	case errors.Is(err, dbconn.ErrInUse):
		httpapi.WriteError(w, http.StatusConflict, "conflict", msgConnInUse, nil)
	case errors.Is(err, dbconn.ErrNotFound):
		httpapi.NotFound(w)
	default:
		h.fail(w, r, err)
	}
}

func (h *Handler) loadConn(w http.ResponseWriter, r *http.Request) (*dbconn.Connection, bool) {
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return nil, false
	}
	c, err := h.Conns.Get(r.Context(), id)
	if errors.Is(err, dbconn.ErrNotFound) {
		httpapi.NotFound(w)
		return nil, false
	}
	if err != nil {
		h.fail(w, r, err)
		return nil, false
	}
	return c, true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	conns, err := h.Conns.List(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]ConnectionDTO, 0, len(conns))
	for _, c := range conns {
		items = append(items, toDTO(c))
	}
	httpapi.WriteJSON(w, http.StatusOK, listResponse{Items: items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in connectionRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, "Format permintaan tidak valid.", nil)
		return
	}
	c, err := h.Conns.Create(r.Context(), in.input())
	if err != nil {
		h.writeConnErr(w, r, err)
		return
	}
	h.audit(r, "connection.created", c.ID, true)
	httpapi.WriteJSON(w, http.StatusCreated, toDTO(c))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	c, ok := h.loadConn(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toDTO(c))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	c, ok := h.loadConn(w, r)
	if !ok {
		return
	}
	var in connectionRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, "Format permintaan tidak valid.", nil)
		return
	}
	input := in.input()
	input.Engine = c.Engine
	updated, err := h.Conns.Update(r.Context(), c.ID, input)
	if err != nil {
		h.writeConnErr(w, r, err)
		return
	}
	h.Manager.Invalidate(c.ID)
	h.audit(r, "connection.updated", c.ID, true)
	httpapi.WriteJSON(w, http.StatusOK, toDTO(updated))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	c, ok := h.loadConn(w, r)
	if !ok {
		return
	}
	grants, err := h.Grants.ListByConnection(r.Context(), c.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.Conns.Delete(r.Context(), c.ID); err != nil {
		h.writeConnErr(w, r, err)
		return
	}
	h.Manager.Invalidate(c.ID)
	for _, g := range grants {
		if err := h.Sessions.RevokeGroup(r.Context(), g.GroupID); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.audit(r, "connection.deleted", c.ID, true)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) testNew(w http.ResponseWriter, r *http.Request) {
	var in connectionRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, "Format permintaan tidak valid.", nil)
		return
	}
	input := in.input()
	input.Normalize()
	if err := input.Validate(true); err != nil {
		h.writeConnErr(w, r, err)
		return
	}
	h.runTest(w, r, 0, dbconn.ParamsFromInput(input))
}

func (h *Handler) testSaved(w http.ResponseWriter, r *http.Request) {
	c, ok := h.loadConn(w, r)
	if !ok {
		return
	}
	var in connectionRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, "Format permintaan tidak valid.", nil)
		return
	}
	input := in.input()
	input.Normalize()
	if err := input.Validate(false); err != nil {
		h.writeConnErr(w, r, err)
		return
	}
	p, err := h.Conns.ParamsForEdit(r.Context(), c.ID, input)
	if errors.Is(err, dbconn.ErrNotFound) {
		httpapi.NotFound(w)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.runTest(w, r, c.ID, p)
}

func (h *Handler) runTest(w http.ResponseWriter, r *http.Request, connID int64, p dbconn.Params) {
	err := h.Manager.Test(r.Context(), p)
	if err == nil {
		h.audit(r, "connection.tested", connID, true)
		httpapi.WriteJSON(w, http.StatusOK, testResponse{Success: true, Message: "Test koneksi berhasil."})
		return
	}
	ce, ok := dbconn.AsConnError(err)
	if !ok {
		h.fail(w, r, err)
		return
	}
	h.audit(r, "connection.tested", connID, false)
	httpapi.WriteJSON(w, http.StatusOK, testResponse{Success: false, Code: ce.Kind.String(), Message: "Test koneksi gagal. " + ce.Kind.UserMessage()})
}

func (h *Handler) audit(r *http.Request, event string, connID int64, success bool) {
	attrs := []any{"event", event, "connection_id", connID, "success", success}
	if u, ok := auth.CurrentUser(r.Context()); ok {
		attrs = append(attrs, "actor_id", u.ID)
	}
	h.Logger.Info("connection api", attrs...)
}
