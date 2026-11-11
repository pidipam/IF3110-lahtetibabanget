// Package spaapi menyajikan API migration sesuai kontrak SPA (web/src/lib/migrations):
// ID berupa string, status migration berupa label, job = satu eksekusi.
// Seluruh logika ada di workflow.Service; paket ini hanya menerjemahkan bentuk data.
package spaapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

type Handler struct {
	Svc    *workflow.Service
	UserID func(r *http.Request) (int64, bool)
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/databases/{dbID}/migrations", h.list)
	mux.HandleFunc("POST /api/databases/{dbID}/migrations", h.create)
	mux.HandleFunc("GET /api/migrations/{id}", h.get)
	mux.HandleFunc("PUT /api/migrations/{id}", h.update)
	mux.HandleFunc("POST /api/migrations/{id}/submit", h.submit)
	mux.HandleFunc("POST /api/migrations/{id}/execute", h.execute)
	mux.HandleFunc("GET /api/databases/{dbID}/history", h.history)
	mux.HandleFunc("GET /api/databases/{dbID}/history/{eid}", h.historyItem)
	mux.HandleFunc("GET /api/jobs/{id}", h.job)
	mux.HandleFunc("POST /api/jobs/{id}/{action}", h.jobAction)
}

// ---- bentuk data SPA ----

type revision struct {
	ID                 string `json:"id"`
	RevisionNumber     int    `json:"revision_number"`
	SQLUp              string `json:"sql_up"`
	SQLDown            string `json:"sql_down,omitempty"`
	IsIrreversible     bool   `json:"is_irreversible"`
	IrreversibleReason string `json:"irreversible_reason,omitempty"`
	IsBatchMode        bool   `json:"is_batch_mode"`
	BatchColumn        string `json:"batch_column,omitempty"`
	BatchSize          int    `json:"batch_size,omitempty"`
	Checksum           string `json:"checksum"`
	CreatedAt          string `json:"created_at"`
}

type migrationDTO struct {
	ID              string   `json:"id"`
	DatabaseID      string   `json:"database_id"`
	Title           string   `json:"title"`
	LinearOrder     int      `json:"linear_order"`
	Status          string   `json:"status"`
	CurrentRevision revision `json:"current_revision"`
	CreatedBy       string   `json:"created_by"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type input struct {
	Title              string  `json:"title"`
	SQLUp              string  `json:"sql_up"`
	SQLDown            *string `json:"sql_down"`
	IsIrreversible     bool    `json:"is_irreversible"`
	IrreversibleReason *string `json:"irreversible_reason"`
	IsBatchMode        bool    `json:"is_batch_mode"`
	BatchColumn        *string `json:"batch_column"`
	BatchSize          *int    `json:"batch_size"`
}

func (in input) request() workflow.CreateRequest {
	s := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	r := workflow.CreateRequest{Title: in.Title, SQLUp: in.SQLUp, SQLDown: s(in.SQLDown), IsIrreversible: in.IsIrreversible,
		IrreversibleReason: s(in.IrreversibleReason), IsBatchMode: in.IsBatchMode, BatchColumn: s(in.BatchColumn)}
	if in.BatchSize != nil {
		r.BatchSize = *in.BatchSize
	}
	return r
}

// StatusLabel menurunkan label status migration dari eksekusi terakhirnya.
func StatusLabel(le *workflow.ExecutionResponse) string {
	if le == nil {
		return "Draft"
	}
	switch le.Status {
	case workflow.StatusPendingReview:
		return "Pending Review"
	case workflow.StatusApproved:
		return "Approved"
	case workflow.StatusQueued:
		return "Pending Queue"
	case workflow.StatusRunning, workflow.StatusPartial, workflow.StatusUncertain, workflow.StatusPartiallyReverted:
		return "Running"
	case workflow.StatusSuccess:
		return "Success"
	case workflow.StatusFailed:
		return "Failed"
	}
	return "Draft" // rejected / cancelled: kembali ke draft
}

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func idStr(n int64) string   { return strconv.FormatInt(n, 10) }

func (h *Handler) toDTOs(ctx context.Context, ms []workflow.MigrationResponse) ([]migrationDTO, error) {
	var ids []int64
	for _, m := range ms {
		ids = append(ids, m.CreatedBy)
	}
	names, err := h.Svc.Usernames(ctx, ids...)
	if err != nil {
		return nil, err
	}
	out := make([]migrationDTO, 0, len(ms))
	for _, m := range ms {
		r := m.Revision
		d := migrationDTO{ID: idStr(m.ID), DatabaseID: idStr(m.DatabaseID), Title: m.Title, Status: StatusLabel(m.LatestExecution),
			CreatedBy: names[m.CreatedBy], CreatedAt: iso(m.CreatedAt), UpdatedAt: iso(m.UpdatedAt),
			CurrentRevision: revision{ID: idStr(m.ID) + "-" + strconv.Itoa(r.RevisionNumber), RevisionNumber: r.RevisionNumber,
				SQLUp: r.SQLUp, SQLDown: r.SQLDown, IsIrreversible: r.IsIrreversible, IrreversibleReason: r.IrreversibleReason,
				IsBatchMode: r.IsBatchMode, BatchColumn: r.BatchColumn, BatchSize: r.BatchSize, Checksum: r.Checksum, CreatedAt: iso(r.CreatedAt)}}
		if m.LinearOrder != nil {
			d.LinearOrder = *m.LinearOrder
		}
		out = append(out, d)
	}
	return out, nil
}

// ---- helper HTTP ----

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg, known := workflow.HTTPError(err)
	switch {
	case status == 422:
		httpapi.Invalid(w, msg, map[string]string{workflow.ValidationField(err): msg})
	case !known:
		if h.Svc.Log != nil {
			h.Svc.Log.Error("api.error", "event", "api.error", "path", r.URL.Path, "error", err.Error())
		}
		httpapi.Internal(w)
	default:
		httpapi.WriteError(w, status, code, msg, nil)
	}
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) (int64, bool) {
	uid, ok := h.UserID(r)
	if !ok {
		httpapi.Unauthenticated(w, false)
	}
	return uid, ok
}

// ids: ID tidak valid -> 404 sebelum cek akses (konvensi M1).
func (h *Handler) ids(w http.ResponseWriter, r *http.Request, names ...string) (uid int64, out []int64, ok bool) {
	if uid, ok = h.user(w, r); !ok {
		return
	}
	for _, n := range names {
		v, good := httpapi.PathID(r, n)
		if !good {
			httpapi.NotFound(w)
			return 0, nil, false
		}
		out = append(out, v)
	}
	return uid, out, true
}

// migrationScope mengurai {id} migration dan mencari database pemiliknya.
func (h *Handler) migrationScope(w http.ResponseWriter, r *http.Request) (uid, dbID, migID int64, ok bool) {
	uid, ids, ok := h.ids(w, r, "id")
	if !ok {
		return
	}
	db, err := h.Svc.Locate(r.Context(), ids[0])
	if err != nil {
		h.fail(w, r, err)
		return 0, 0, 0, false
	}
	return uid, db, ids[0], true
}

func (h *Handler) one(w http.ResponseWriter, r *http.Request, uid, dbID, migID int64, code int) {
	m, err := h.Svc.Get(r.Context(), uid, dbID, migID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d, err := h.toDTOs(r.Context(), []workflow.MigrationResponse{m})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, code, d[0])
}

// ---- migration ----

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, ids, ok := h.ids(w, r, "dbID")
	if !ok {
		return
	}
	ms, err := h.Svc.List(r.Context(), uid, ids[0])
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d, err := h.toDTOs(r.Context(), ms)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, d)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	uid, ids, ok := h.ids(w, r, "dbID")
	if !ok {
		return
	}
	var in input
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, err.Error(), nil)
		return
	}
	m, err := h.Svc.Create(r.Context(), uid, ids[0], in.request())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.one(w, r, uid, ids[0], m.ID, 201)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	uid, db, id, ok := h.migrationScope(w, r)
	if !ok {
		return
	}
	h.one(w, r, uid, db, id, 200)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	uid, db, id, ok := h.migrationScope(w, r)
	if !ok {
		return
	}
	var in input
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, err.Error(), nil)
		return
	}
	if _, err := h.Svc.Revise(r.Context(), uid, db, id, in.request()); err != nil {
		h.fail(w, r, err)
		return
	}
	h.one(w, r, uid, db, id, 200)
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	uid, db, id, ok := h.migrationScope(w, r)
	if !ok {
		return
	}
	if _, err := h.Svc.Submit(r.Context(), uid, db, id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.one(w, r, uid, db, id, 200)
}

// execute: migration yang sudah disetujui masuk antrean.
func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	uid, db, id, ok := h.migrationScope(w, r)
	if !ok {
		return
	}
	m, err := h.Svc.Get(r.Context(), uid, db, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if m.LatestExecution == nil || m.LatestExecution.Status != workflow.StatusApproved {
		httpapi.WriteError(w, 409, "invalid_status", "Migration harus disetujui sebelum dijalankan.", nil)
		return
	}
	res, err := h.Svc.Enqueue(r.Context(), uid, db, m.LatestExecution.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 202, map[string]string{"job_id": idStr(res.ExecutionID), "message": "Migration masuk antrean."})
}

// ---- history ----

type historyDTO struct {
	ExecutionID         string `json:"execution_id"`
	MigrationID         string `json:"migration_id"`
	RevisionID          string `json:"revision_id"`
	ExecutionType       string `json:"execution_type"`
	ExecutedBy          string `json:"executed_by"`
	ExecutedAt          string `json:"executed_at"`
	SQLExecuted         string `json:"sql_executed"`
	RevertedExecutionID string `json:"reverted_execution_id,omitempty"`
}

var typeLabel = map[workflow.Kind]string{workflow.KindUp: "APPLY", workflow.KindRollback: "ROLLBACK", workflow.KindForward: "FORWARD"}

// historyAll: semua riwayat database, terbaru di atas.
func (h *Handler) historyAll(ctx context.Context, uid, dbID int64, hide bool) ([]historyDTO, error) {
	hs, err := h.Svc.History(ctx, uid, dbID, hide)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, x := range hs {
		ids = append(ids, x.ExecutedBy)
	}
	names, err := h.Svc.Usernames(ctx, ids...)
	if err != nil {
		return nil, err
	}
	out := make([]historyDTO, 0, len(hs))
	for i := len(hs) - 1; i >= 0; i-- {
		x := hs[i]
		d := historyDTO{ExecutionID: idStr(x.ExecutionID), MigrationID: idStr(x.MigrationID),
			RevisionID: idStr(x.MigrationID) + "-" + strconv.Itoa(x.RevisionNumber), ExecutionType: typeLabel[x.ExecutionType],
			ExecutedBy: names[x.ExecutedBy], ExecutedAt: iso(x.ExecutedAt), SQLExecuted: x.SQLExecuted}
		if x.RevertedExecutionID != nil {
			d.RevertedExecutionID = idStr(*x.RevertedExecutionID)
		}
		out = append(out, d)
	}
	return out, nil
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	uid, ids, ok := h.ids(w, r, "dbID")
	if !ok {
		return
	}
	p, ok := httpapi.ParsePage(w, r)
	if !ok {
		return
	}
	hide := r.URL.Query().Get("hide_rollback") == "true"
	all, err := h.historyAll(r.Context(), uid, ids[0], hide)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	lo, hi := p.Offset(), p.Offset()+p.PageSize
	if lo > len(all) {
		lo = len(all)
	}
	if hi > len(all) {
		hi = len(all)
	}
	httpapi.WriteJSON(w, 200, httpapi.NewPage(all[lo:hi], p, len(all)))
}

func (h *Handler) historyItem(w http.ResponseWriter, r *http.Request) {
	uid, ids, ok := h.ids(w, r, "dbID", "eid")
	if !ok {
		return
	}
	all, err := h.historyAll(r.Context(), uid, ids[0], false)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	for _, x := range all {
		if x.ExecutionID == idStr(ids[1]) {
			httpapi.WriteJSON(w, 200, x)
			return
		}
	}
	httpapi.NotFound(w)
}

// ---- job (= satu eksekusi yang sudah masuk antrean atau berjalan) ----

type jobDTO struct {
	JobID            string   `json:"job_id"`
	ExecutionID      string   `json:"execution_id"`
	MigrationID      string   `json:"migration_id"`
	DatabaseID       string   `json:"database_id"`
	Title            string   `json:"title"`
	Kind             string   `json:"kind"`
	Status           string   `json:"status"`
	Attempt          int      `json:"attempt"`
	IsBatchMode      bool     `json:"is_batch_mode"`
	BatchesCommitted int      `json:"batches_committed"`
	BatchesTotal     *int     `json:"batches_total"`
	RowsCommitted    int64    `json:"rows_committed"`
	RowsTotal        *int64   `json:"rows_total"`
	ErrorMessage     string   `json:"error_message"`
	QueuedAt         *string  `json:"queued_at"`
	StartedAt        *string  `json:"started_at"`
	FinishedAt       *string  `json:"finished_at"`
	UpdatedAt        string   `json:"updated_at"`
	AllowedActions   []string `json:"allowed_actions"`
}

func tp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := iso(*t)
	return &s
}

// allowed: aksi yang masuk akal untuk status job, dibatasi hak edit dan posisi migration.
func allowed(e workflow.Execution, rev workflow.Revision, m workflow.MigrationResponse, canEdit bool) []string {
	out := []string{}
	if !canEdit {
		return out
	}
	switch e.Status {
	case workflow.StatusQueued, workflow.StatusRunning:
		out = append(out, "cancel")
	case workflow.StatusPartial, workflow.StatusUncertain:
		out = append(out, "resume")
		if rev.IsBatchMode && e.Kind != workflow.KindRollback {
			out = append(out, "rollback")
		}
	case workflow.StatusPartiallyReverted:
		out = append(out, "rollback")
	case workflow.StatusFailed:
		out = append(out, "retry")
	case workflow.StatusSuccess:
		if e.Kind == workflow.KindRollback {
			if m.CanForward {
				out = append(out, "forward")
			}
		} else if m.CanRollback {
			out = append(out, "rollback")
		}
	}
	return out
}

func (h *Handler) job(w http.ResponseWriter, r *http.Request) {
	uid, ids, ok := h.ids(w, r, "id")
	if !ok {
		return
	}
	db, mig, err := h.Svc.LocateExecution(r.Context(), ids[0])
	if err != nil {
		h.fail(w, r, err)
		return
	}
	m, err := h.Svc.Get(r.Context(), uid, db, mig) // juga memeriksa hak lihat
	if err != nil {
		h.fail(w, r, err)
		return
	}
	e, err := h.Svc.Repo.GetExecutionScoped(r.Context(), db, ids[0])
	if err != nil {
		h.fail(w, r, err)
		return
	}
	switch e.Status {
	case workflow.StatusPendingReview, workflow.StatusApproved, workflow.StatusRejected:
		httpapi.NotFound(w) // belum menjadi job
		return
	}
	rev, err := h.Svc.Repo.GetRevision(r.Context(), mig, e.RevisionNumber)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d := jobDTO{JobID: idStr(e.ID), ExecutionID: idStr(e.ID), MigrationID: idStr(mig), DatabaseID: idStr(db), Title: m.Title,
		Kind: string(e.Kind), Status: string(e.Status), Attempt: e.Attempt, IsBatchMode: rev.IsBatchMode,
		BatchesCommitted: e.BatchesCommitted, RowsCommitted: e.RowsCommitted, ErrorMessage: e.ErrorMessage,
		QueuedAt: tp(e.QueuedAt), StartedAt: tp(e.StartedAt), FinishedAt: tp(e.FinishedAt), UpdatedAt: iso(e.UpdatedAt),
		AllowedActions: allowed(e, rev, m, h.Svc.CanEdit(r.Context(), uid, db))}
	httpapi.WriteJSON(w, 200, d)
}

func (h *Handler) jobAction(w http.ResponseWriter, r *http.Request) {
	uid, ids, ok := h.ids(w, r, "id")
	if !ok {
		return
	}
	action := r.PathValue("action")
	switch action {
	case "cancel", "resume", "retry", "rollback", "forward":
	default:
		httpapi.NotFound(w)
		return
	}
	ctx := r.Context()
	db, mig, err := h.Svc.LocateExecution(ctx, ids[0])
	if err != nil {
		h.fail(w, r, err)
		return
	}
	e, err := h.Svc.Repo.GetExecutionScoped(ctx, db, ids[0])
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res := map[string]any{"migration_id": idStr(mig), "needs_approval": false}
	switch action {
	case "cancel":
		_, err = h.Svc.Cancel(ctx, uid, db, e.ID)
		res["message"] = "Job dibatalkan. Batch yang sudah di-commit tetap tersimpan."
	case "resume", "retry":
		if (action == "resume") != (e.Status == workflow.StatusPartial || e.Status == workflow.StatusUncertain) {
			err = workflow.ErrNotAllowed
			break
		}
		var q workflow.ExecuteResponse
		if q, err = h.Svc.Enqueue(ctx, uid, db, e.ID); err == nil {
			res["job_id"] = idStr(q.ExecutionID)
			res["message"] = map[string]string{"resume": "Job dilanjutkan dari checkpoint terakhir.", "retry": "Job dijalankan ulang."}[action]
		}
	case "rollback":
		if e.Status == workflow.StatusPartial || e.Status == workflow.StatusUncertain || e.Status == workflow.StatusPartiallyReverted {
			var q workflow.ExecuteResponse
			if q, err = h.Svc.RevertBatches(ctx, uid, db, e.ID); err == nil {
				res["job_id"] = idStr(q.ExecutionID)
				res["message"] = "Pembatalan batch yang sudah di-commit diantrekan."
			}
			break
		}
		if e.Status != workflow.StatusSuccess {
			err = workflow.ErrNotAllowed
			break
		}
		if _, err = h.Svc.Rollback(ctx, uid, db, mig); err == nil {
			res["needs_approval"] = true
			res["message"] = "Rollback diajukan dan menunggu persetujuan leader grup."
		}
	case "forward":
		if e.Status != workflow.StatusSuccess || e.Kind != workflow.KindRollback {
			err = workflow.ErrNotAllowed
			break
		}
		if _, err = h.Svc.Submit(ctx, uid, db, mig); err == nil {
			res["needs_approval"] = true
			res["message"] = "Forward diajukan dan menunggu persetujuan leader grup."
		}
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, res)
}
