package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/approval"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/exec"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/spaapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

// wfAccess menghubungkan authz M1 ke workflow.Access (satu sumber kebenaran untuk hak akses).
type wfAccess struct{ az *authz.Authorizer }

func (a wfAccess) Can(ctx context.Context, uid, dbID int64, edit bool) error {
	act := authz.ViewMigration
	if edit {
		act = authz.EditMigration
	}
	err := a.az.Can(ctx, authz.Principal{UserID: uid}, dbID, act)
	if errors.Is(err, authz.ErrForbidden) {
		return workflow.ErrForbidden
	}
	return err
}

// wfApprovals menghubungkan modul approval (helen) ke workflow.Approvals.
// Pemungutan suara terjadi di approval.Service; workflow hanya perlu tahu apakah sudah cukup.
type wfApprovals struct{ svc *approval.Service }

func (wfApprovals) Open(context.Context, int64, int64) error { return nil }

func (a wfApprovals) Satisfied(ctx context.Context, execID int64) (bool, error) {
	st, err := a.svc.Status(ctx, execID)
	if err != nil {
		return false, err
	}
	return st.State == approval.StateApproved, nil
}

// initWorkflow dipanggil dari New setelah Authz, Manager, dan Approvals siap.
// Options.Queue (antrean Vara) boleh nil: dipakai antrean lokal sementara.
func (a *App) initWorkflow(opt Options, targets managerTargets) {
	repo := workflow.NewRepository(opt.DB)
	access := wfAccess{a.Authz}
	approvals := wfApprovals{a.Approvals}
	queue := opt.Queue
	if queue == nil {
		queue = &localQueue{a: a}
	}
	a.Workflow = workflow.NewService(repo, access, approvals, queue, a.Logger)
	a.wfHandler = &exec.Handler{Repo: repo, Targets: targets, Access: access, Approvals: approvals,
		Queue: queue, Log: a.Logger, WorkerID: fmt.Sprintf("app-%d", time.Now().UnixNano()%100000)}
	a.wfAPI = &spaapi.Handler{Svc: a.Workflow, UserID: func(r *http.Request) (int64, bool) {
		p, ok := auth.CurrentPrincipal(r.Context())
		return p.UserID, ok
	}}
	// Penolakan melepas nomor urut migration yang belum pernah berjalan.
	a.Approvals.OnRejected = func(ctx context.Context, execID int64) {
		if err := a.Workflow.ReleaseRejected(ctx, execID); err != nil {
			a.Logger.Error("gagal melepas nomor urut", "execution", execID, "err", err)
		}
	}
}

func (a *App) workflowRoutes() { a.wfAPI.RegisterRoutes(a.mux) }

// StartWorkflowRecovery memulihkan eksekusi 'running' yang lease-nya habis (worker mati).
func (a *App) StartWorkflowRecovery(ctx context.Context) {
	go a.wfHandler.RunRecovery(ctx, 15*time.Second)
}

// WorkflowHandler dipakai cmd/worker bila worker dipisah dari server.
func (a *App) WorkflowHandler() *exec.Handler { return a.wfHandler }

// localQueue: PENGGANTI SEMENTARA antrean Vara. Menjalankan exec.Handler di goroutine
// dalam proses server. Ganti dengan antrean Vara lewat Options.Queue.
type localQueue struct {
	a *App
	n atomic.Int64
}

func (q *localQueue) Enqueue(_ context.Context, execID int64, delay time.Duration) (string, error) {
	job := fmt.Sprintf("local-%d-%d", execID, q.n.Add(1))
	time.AfterFunc(delay, func() {
		if err := q.a.wfHandler.Handle(context.Background(), execID); err != nil {
			q.a.Logger.Error("worker lokal gagal", "execution", execID, "err", err)
		}
	})
	return job, nil
}
