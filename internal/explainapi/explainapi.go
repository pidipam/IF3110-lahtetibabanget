package explainapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

type Migrations interface {
	Locate(ctx context.Context, migID int64) (int64, error)
	Get(ctx context.Context, uid, dbID, id int64) (workflow.MigrationResponse, error)
}

type Explainer interface {
	Explain(ctx context.Context, dbID int64, sql string, analyze bool) (*migration.Explanation, error)
}

type Authorizer interface {
	Can(ctx context.Context, p authz.Principal, dbID int64, a authz.Action) error
}

type Handler struct {
	Migrations Migrations
	Explainer Explainer
	Authz Authorizer
	Logger *slog.Logger
}

type request struct {
	Analyze bool `json:"analyze"`
}

type nodeDTO struct {
	Depth int `json:"depth"`
	Prefix string `json:"prefix"`
	Node string `json:"node"`
	Relation string `json:"relation"`
	Index string `json:"index"`
	Join string `json:"join"`
	StartupCost float64 `json:"startup_cost"`
	TotalCost float64 `json:"total_cost"`
	PlanRows float64 `json:"plan_rows"`
	HasActual bool `json:"has_actual"`
	ActualRows float64 `json:"actual_rows"`
	ActualTime float64 `json:"actual_time"`
	Loops float64 `json:"loops"`
}

type statementDTO struct {
	Number int `json:"number"`
	SQL string `json:"sql"`
	NoPlan bool `json:"no_plan"`
	Error string `json:"error"`
	Nodes []nodeDTO `json:"nodes"`
	HasTiming bool `json:"has_timing"`
	PlanningTime float64 `json:"planning_time"`
	ExecutionTime float64 `json:"execution_time"`
}

type explanationDTO struct {
	Analyze bool `json:"analyze"`
	RevisionNumber int `json:"revision_number"`
	Stopped bool `json:"stopped"`
	Statements []statementDTO `json:"statements"`
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireLogin func(http.Handler) http.Handler) {
	mux.Handle("POST /api/migrations/{id}/explain", requireLogin(http.HandlerFunc(h.explain)))
}

func (h *Handler) explain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := auth.CurrentPrincipal(ctx)
	if !ok {
		httpapi.Unauthenticated(w, false)
		return
	}
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return
	}
	var in request
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, err.Error(), nil)
		return
	}
	dbID, err := h.Migrations.Locate(ctx, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	m, err := h.Migrations.Get(ctx, p.UserID, dbID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.Authz.Can(ctx, p, dbID, authz.EditMigration); err != nil {
		h.fail(w, r, err)
		return
	}
	if !m.CanEdit {
		httpapi.WriteError(w, http.StatusConflict, "invalid_status", "Analisis plan hanya untuk migration yang belum dijalankan.", nil)
		return
	}
	ex, err := h.Explainer.Explain(ctx, dbID, m.Revision.SQLUp, in.Analyze)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("migration explained", "user_id", p.UserID, "db_id", dbID, "migration_id", id, "analyze", in.Analyze)
	httpapi.WriteJSON(w, http.StatusOK, toDTO(ex, m.Revision.RevisionNumber))
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, workflow.ErrForbidden):
		httpapi.Forbidden(w)
	case errors.Is(err, workflow.ErrNotFound), errors.Is(err, migration.ErrTargetNotFound):
		httpapi.NotFound(w)
	case errors.Is(err, migration.ErrExplainUnsupported):
		httpapi.WriteError(w, http.StatusUnprocessableEntity, "explain_unsupported", err.Error(), nil)
	case errors.Is(err, migration.ErrTargetUnavailable):
		msg := migration.ErrTargetUnavailable.Error()
		if ce, ok := dbconn.AsConnError(err); ok {
			msg = ce.Kind.UserMessage()
		}
		httpapi.WriteError(w, http.StatusServiceUnavailable, "target_unavailable", msg, nil)
	default:
		h.Logger.Error("request failed", "method", r.Method, "path", httpapi.LogPath(r), "err", err)
		httpapi.Internal(w)
	}
}

func toDTO(ex *migration.Explanation, revision int) explanationDTO {
	out := explanationDTO{Analyze: ex.Analyze, RevisionNumber: revision, Stopped: ex.Stopped, Statements: make([]statementDTO, 0, len(ex.Statements))}
	for _, s := range ex.Statements {
		st := statementDTO{Number: s.Number, SQL: s.SQL, NoPlan: s.NoPlan, Error: s.Error, Nodes: make([]nodeDTO, 0, len(s.Nodes)), HasTiming: s.HasTiming, PlanningTime: s.PlanningTime, ExecutionTime: s.ExecutionTime}
		for _, n := range s.Nodes {
			st.Nodes = append(st.Nodes, nodeDTO{Depth: n.Depth, Prefix: n.Prefix, Node: n.Node, Relation: n.Relation, Index: n.Index, Join: n.Join, StartupCost: n.StartupCost, TotalCost: n.TotalCost, PlanRows: n.PlanRows, HasActual: n.HasActual, ActualRows: n.ActualRows, ActualTime: n.ActualTime, Loops: n.Loops})
		}
		out.Statements = append(out.Statements, st)
	}
	return out
}
