package workflow

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// HTTP memasang API JSON. UserID dan penanganan sesi/CSRF disuplai aplikasi
// (LoadSession sudah memeriksa CSRF untuk non-GET lewat header X-CSRF-Token).
type HTTP struct {
	Svc    *Service
	UserID func(r *http.Request) (int64, bool)
}

const maxBody = 512 << 10

func (h *HTTP) RegisterRoutes(mux *http.ServeMux) {
	b := "/api/databases/{dbID}"
	mux.HandleFunc("GET "+b+"/migrations", h.list)
	mux.HandleFunc("POST "+b+"/migrations", h.create)
	mux.HandleFunc("GET "+b+"/migrations/{id}", h.get)
	mux.HandleFunc("PATCH "+b+"/migrations/{id}", h.revise)
	mux.HandleFunc("POST "+b+"/migrations/{id}/submit", h.submit)
	mux.HandleFunc("POST "+b+"/migrations/{id}/rollback", h.rollback)
	mux.HandleFunc("GET "+b+"/executions", h.executions)
	mux.HandleFunc("GET "+b+"/executions/{eid}", h.execution)
	mux.HandleFunc("POST "+b+"/executions/{eid}/queue", h.queue)
	mux.HandleFunc("POST "+b+"/executions/{eid}/resume", h.queue)
	mux.HandleFunc("POST "+b+"/executions/{eid}/retry", h.queue)
	mux.HandleFunc("POST "+b+"/executions/{eid}/revert-batches", h.revert)
	mux.HandleFunc("POST "+b+"/executions/{eid}/cancel", h.cancel)
	mux.HandleFunc("GET "+b+"/history", h.history)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, c, msg string) {
	writeJSON(w, code, ErrorResponse{Error: ErrorBody{Code: c, Message: msg}})
}

func (h *HTTP) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		writeErr(w, 403, "forbidden", "akses ditolak")
	case errors.Is(err, ErrNotFound):
		writeErr(w, 404, "not_found", "tidak ditemukan")
	case IsValidation(err):
		writeErr(w, 422, "validation_failed", err.Error())
	case errors.Is(err, ErrImmutable), errors.Is(err, ErrConflict), errors.Is(err, ErrNotAllowed),
		errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrNotNextInLine), errors.Is(err, ErrNotLastActive),
		errors.Is(err, ErrIrreversible), errors.Is(err, ErrNotActive), errors.Is(err, ErrAlreadyActive),
		errors.Is(err, ErrNotSubmitted), errors.Is(err, ErrForwardNeedsUndo):
		writeErr(w, 409, "conflict", err.Error())
	default:
		// detail internal (SQL, DSN) tidak boleh bocor ke klien
		h.Svc.Log.Error("api.error", "event", "api.error", "error", err.Error())
		writeErr(w, 500, "internal", "terjadi kesalahan internal")
	}
}

// ids mengurai path; ID tak valid → 404 sebelum cek akses (konsisten dengan M1).
func (h *HTTP) ids(w http.ResponseWriter, r *http.Request, names ...string) (uid int64, out []int64, ok bool) {
	uid, authed := h.UserID(r)
	if !authed {
		writeErr(w, 401, "unauthorized", "perlu login")
		return
	}
	for _, n := range names {
		v, err := strconv.ParseInt(r.PathValue(n), 10, 64)
		if err != nil || v <= 0 {
			writeErr(w, 404, "not_found", "tidak ditemukan")
			return
		}
		out = append(out, v)
	}
	return uid, out, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, 400, "bad_request", "isi permintaan tidak valid")
		return false
	}
	return true
}

func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID")
	if !ok {
		return
	}
	v, err := h.Svc.List(r.Context(), uid, id[0])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID")
	if !ok {
		return
	}
	var req CreateRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := h.Svc.Create(r.Context(), uid, id[0], req)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 201, v)
}

func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "id")
	if !ok {
		return
	}
	v, err := h.Svc.Get(r.Context(), uid, id[0], id[1])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *HTTP) revise(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "id")
	if !ok {
		return
	}
	var req ReviseRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := h.Svc.Revise(r.Context(), uid, id[0], id[1], req)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *HTTP) submit(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "id")
	if !ok {
		return
	}
	v, err := h.Svc.Submit(r.Context(), uid, id[0], id[1])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 201, v)
}

func (h *HTTP) rollback(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "id")
	if !ok {
		return
	}
	v, err := h.Svc.Rollback(r.Context(), uid, id[0], id[1])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 201, v)
}

func (h *HTTP) executions(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID")
	if !ok {
		return
	}
	v, err := h.Svc.Executions(r.Context(), uid, id[0])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *HTTP) execution(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "eid")
	if !ok {
		return
	}
	v, err := h.Svc.Execution(r.Context(), uid, id[0], id[1])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *HTTP) queue(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "eid")
	if !ok {
		return
	}
	v, err := h.Svc.Enqueue(r.Context(), uid, id[0], id[1])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 202, v)
}

func (h *HTTP) revert(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "eid")
	if !ok {
		return
	}
	v, err := h.Svc.RevertBatches(r.Context(), uid, id[0], id[1])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 202, v)
}

func (h *HTTP) cancel(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID", "eid")
	if !ok {
		return
	}
	v, err := h.Svc.Cancel(r.Context(), uid, id[0], id[1])
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *HTTP) history(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := h.ids(w, r, "dbID")
	if !ok {
		return
	}
	hide := r.URL.Query().Get("hide_rollback") == "true"
	v, err := h.Svc.History(r.Context(), uid, id[0], hide)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
