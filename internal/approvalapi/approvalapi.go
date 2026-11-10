package approvalapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/approval"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
)

type Handler struct {
	Approvals *approval.Service
	Authz *authz.Authorizer
	Logger *slog.Logger
}

type UserRef struct {
	ID int64 `json:"id"`
	Username string `json:"username"`
}

type DatabaseRef struct {
	ID int64 `json:"id"`
	Name string `json:"name"`
}

type GroupApprovalDTO struct {
	GroupID int64 `json:"group_id"`
	GroupName string `json:"group_name"`
	Level string `json:"level"`
	Leader *UserRef `json:"leader"`
	Decision string `json:"decision"`
	DecidedBy *UserRef `json:"decided_by"`
	DecidedAt *time.Time `json:"decided_at"`
	Reason string `json:"reason,omitempty"`
}

type ApprovalDTO struct {
	ExecutionID int64 `json:"execution_id"`
	MigrationID int64 `json:"migration_id"`
	Title string `json:"title"`
	Database DatabaseRef `json:"database"`
	Kind string `json:"kind"`
	RevisionNumber int `json:"revision_number"`
	CurrentRevision int `json:"current_revision"`
	ExecutionStatus string `json:"execution_status"`
	RequestedBy UserRef `json:"requested_by"`
	CreatedAt time.Time `json:"created_at"`
	State string `json:"state"`
	Message string `json:"message"`
	Groups []GroupApprovalDTO `json:"groups"`
	CanDecide bool `json:"can_decide"`
}

type decisionRequest struct {
	RevisionNumber int `json:"revision_number"`
	Reason string `json:"reason"`
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireLogin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/approvals", requireLogin(http.HandlerFunc(h.list)))
	mux.Handle("GET /api/migrations/{id}/approvals", requireLogin(http.HandlerFunc(h.status)))
	mux.Handle("POST /api/migrations/{id}/approve", requireLogin(http.HandlerFunc(h.approve)))
	mux.Handle("POST /api/migrations/{id}/reject", requireLogin(http.HandlerFunc(h.reject)))
}

func principal(r *http.Request) authz.Principal {
	u, _ := auth.CurrentUser(r.Context())
	return authz.Principal{UserID: u.ID, IsAdmin: u.IsAdmin}
}

func toDTO(st approval.Status, userID int64) ApprovalDTO {
	e := st.Execution
	out := ApprovalDTO{ExecutionID: e.ID, MigrationID: e.MigrationID, Title: e.Title, Database: DatabaseRef{ID: e.DatabaseID, Name: e.DatabaseName}, Kind: e.Kind, RevisionNumber: e.RevisionNumber, CurrentRevision: e.CurrentRevision, ExecutionStatus: e.Status, RequestedBy: UserRef{ID: e.RequestedBy, Username: e.RequestedByName}, CreatedAt: e.CreatedAt.UTC(), State: string(st.State), Message: st.Message, Groups: make([]GroupApprovalDTO, 0, len(st.Groups))}
	for _, g := range st.Groups {
		dto := GroupApprovalDTO{GroupID: g.ID, GroupName: g.Name, Level: g.Level.String(), Decision: string(g.Decision)}
		if g.HasLeader() {
			dto.Leader = &UserRef{ID: g.LeaderID, Username: g.LeaderName}
		}
		if g.Vote != nil {
			at := g.Vote.DecidedAt.UTC()
			dto.DecidedBy = &UserRef{ID: g.Vote.ApproverID, Username: g.Vote.ApproverName}
			dto.DecidedAt = &at
			dto.Reason = g.Vote.Reason
		}
		if g.Decision == approval.DecisionPending && g.HasLeader() && g.LeaderID == userID {
			out.CanDecide = true
		}
		out.Groups = append(out.Groups, dto)
	}
	if e.Status != "pending_review" || e.RequestedBy == userID || st.State == approval.StateStale || st.State == approval.StateRejected {
		out.CanDecide = false
	}
	return out
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, ok := httpapi.ParsePage(w, r)
	if !ok {
		return
	}
	mine := false
	switch r.URL.Query().Get("mine") {
	case "", "false":
	case "true":
		mine = true
	default:
		httpapi.Invalid(w, "Parameter tidak valid.", map[string]string{"mine": "Isi true atau false."})
		return
	}
	pr := principal(r)
	list, total, err := h.Approvals.Pending(r.Context(), pr, mine, p.PageSize, p.Offset())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]ApprovalDTO, 0, len(list))
	for _, st := range list {
		items = append(items, toDTO(st, pr.UserID))
	}
	httpapi.WriteJSON(w, http.StatusOK, httpapi.NewPage(items, p, total))
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return
	}
	st, err := h.Approvals.StatusForMigration(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	pr := principal(r)
	if err := h.Authz.Can(r.Context(), pr, st.Execution.DatabaseID, authz.ViewMigration); err != nil {
		h.writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toDTO(st, pr.UserID))
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, false)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, true)
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request, reject bool) {
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return
	}
	var in decisionRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, "Format permintaan tidak valid.", nil)
		return
	}
	if in.RevisionNumber <= 0 {
		httpapi.Invalid(w, "Data keputusan tidak valid.", map[string]string{"revision_number": "Isi nomor revisi yang Anda tinjau."})
		return
	}
	if !reject && in.Reason != "" {
		httpapi.Invalid(w, "Data keputusan tidak valid.", map[string]string{"reason": "Alasan hanya untuk penolakan."})
		return
	}
	pr := principal(r)
	var st approval.Status
	var err error
	if reject {
		st, err = h.Approvals.Reject(r.Context(), pr, id, in.RevisionNumber, in.Reason)
	} else {
		st, err = h.Approvals.Approve(r.Context(), pr, id, in.RevisionNumber)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.Logger.Info("migration decision", "user_id", pr.UserID, "migration_id", id, "execution_id", st.Execution.ID, "reject", reject, "state", string(st.State))
	httpapi.WriteJSON(w, http.StatusOK, toDTO(st, pr.UserID))
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, approval.ErrNotFound):
		httpapi.NotFound(w)
	case errors.Is(err, approval.ErrNoPending):
		httpapi.WriteError(w, http.StatusConflict, "no_pending_review", "Tidak ada permintaan yang menunggu persetujuan.", nil)
	case errors.Is(err, approval.ErrRevisionMismatch):
		httpapi.WriteError(w, http.StatusConflict, "revision_changed", "Revisi sudah berubah. Muat ulang halaman sebelum memutuskan.", nil)
	case errors.Is(err, approval.ErrAlreadyDecided):
		httpapi.WriteError(w, http.StatusConflict, "already_decided", "Grup Anda sudah memberi keputusan untuk revisi ini.", nil)
	case errors.Is(err, approval.ErrSelfApproval):
		httpapi.WriteError(w, http.StatusForbidden, "self_approval", "Pengaju tidak boleh memutuskan permintaannya sendiri. Minta leader lain.", nil)
	case errors.Is(err, approval.ErrNotApprover):
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Hanya leader Grup yang punya akses ke database ini yang bisa memutuskan.", nil)
	case errors.Is(err, authz.ErrForbidden):
		httpapi.Forbidden(w)
	case errors.Is(err, approval.ErrReasonRequired):
		httpapi.Invalid(w, "Data keputusan tidak valid.", map[string]string{"reason": "Alasan penolakan wajib diisi."})
	case errors.Is(err, approval.ErrReasonTooLong):
		httpapi.Invalid(w, "Data keputusan tidak valid.", map[string]string{"reason": "Alasan penolakan maksimal 500 karakter."})
	default:
		h.fail(w, r, err)
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	httpapi.Internal(w)
}
