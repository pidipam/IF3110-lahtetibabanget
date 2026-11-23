package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
)

// Access: nil bila pengguna boleh. edit=false → minimal View; edit=true → View+Edit.
// Adapter memetakan authz.ErrForbidden menjadi ErrForbidden.
type Access interface {
	Can(ctx context.Context, userID, dbID int64, edit bool) error
}

// Approvals diimplementasikan modul approval (helen).
type Approvals interface {
	// Open membuat baris approval per group untuk eksekusi (dipanggil setelah eksekusi dibuat).
	Open(ctx context.Context, executionID, databaseID int64) error
	// Satisfied: semua leader group yang berhak sudah menyetujui.
	Satisfied(ctx context.Context, executionID int64) (bool, error)
}

// Queue diimplementasikan modul antrean (Vara). Mengembalikan job ID.
type Queue interface {
	Enqueue(ctx context.Context, executionID int64, delay time.Duration) (string, error)
}

type Service struct {
	Repo      *Repository
	Access    Access
	Approvals Approvals
	Queue     Queue
	Log       *slog.Logger
}

func NewService(r *Repository, a Access, ap Approvals, q Queue, l *slog.Logger) *Service {
	if l == nil {
		l = slog.Default()
	}
	return &Service{Repo: r, Access: a, Approvals: ap, Queue: q, Log: l}
}

func (in CreateRequest) input() RevisionInput {
	return RevisionInput{Title: in.Title, SQLUp: in.SQLUp, SQLDown: in.SQLDown, IsIrreversible: in.IsIrreversible,
		IrreversibleReason: in.IrreversibleReason, IsBatchMode: in.IsBatchMode, BatchColumn: in.BatchColumn, BatchSize: in.BatchSize}
}

// IsValidation memberi tahu lapisan HTTP bahwa error adalah kesalahan input (422).
func IsValidation(err error) bool {
	for _, e := range []error{migration.ErrEmptyTitle, migration.ErrTitleTooLong, migration.ErrEmptySQL, migration.ErrSQLTooLong, migration.ErrInvalidSQL, migration.ErrTxControl,
		ErrDownRequired, ErrReasonRequired, ErrBadBatchColumn, ErrBadBatchSize, ErrBatchSQLShape, ErrBatchDownShape, ErrBatchNotReversible} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func (s *Service) check(ctx context.Context, uid, dbID int64, edit bool) error {
	return s.Access.Can(ctx, uid, dbID, edit)
}

func (s *Service) Create(ctx context.Context, uid, dbID int64, req CreateRequest) (MigrationResponse, error) {
	if err := s.check(ctx, uid, dbID, true); err != nil {
		return MigrationResponse{}, err
	}
	in := req.input()
	if err := checkTitle(in.Title); err != nil {
		return MigrationResponse{}, err
	}
	if err := in.Validate(); err != nil {
		return MigrationResponse{}, err
	}
	m, err := s.Repo.Create(ctx, dbID, uid, in)
	if err != nil {
		return MigrationResponse{}, err
	}
	s.Log.InfoContext(ctx, "migration.created", "event", "migration.created", "actor", uid, "database", dbID, "migration", m.ID)
	return s.view(ctx, uid, m, nil)
}

func (s *Service) Revise(ctx context.Context, uid, dbID, id int64, req ReviseRequest) (MigrationResponse, error) {
	if err := s.check(ctx, uid, dbID, true); err != nil {
		return MigrationResponse{}, err
	}
	in := req.input()
	if err := checkTitle(in.Title); err != nil {
		return MigrationResponse{}, err
	}
	if err := in.Validate(); err != nil {
		return MigrationResponse{}, err
	}
	m, err := s.Repo.Revise(ctx, dbID, id, uid, in)
	if err != nil {
		return MigrationResponse{}, err
	}
	s.Log.InfoContext(ctx, "migration.revised", "event", "migration.revised", "actor", uid, "database", dbID, "migration", m.ID, "revision", m.CurrentRevision)
	return s.view(ctx, uid, m, nil)
}

func (s *Service) Get(ctx context.Context, uid, dbID, id int64) (MigrationResponse, error) {
	if err := s.check(ctx, uid, dbID, false); err != nil {
		return MigrationResponse{}, err
	}
	m, err := s.Repo.Get(ctx, dbID, id)
	if err != nil {
		return MigrationResponse{}, err
	}
	return s.view(ctx, uid, m, nil)
}

func (s *Service) List(ctx context.Context, uid, dbID int64) ([]MigrationResponse, error) {
	if err := s.check(ctx, uid, dbID, false); err != nil {
		return nil, err
	}
	ms, err := s.Repo.List(ctx, dbID)
	if err != nil {
		return nil, err
	}
	items, err := s.Repo.ItemsFor(ctx, dbID)
	if err != nil {
		return nil, err
	}
	out := make([]MigrationResponse, 0, len(ms))
	for _, m := range ms {
		v, err := s.view(ctx, uid, m, items)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// view membangun respons beserta flag aksi. items boleh nil (dimuat sendiri).
func (s *Service) view(ctx context.Context, uid int64, m Migration, items []Item) (MigrationResponse, error) {
	if items == nil {
		var err error
		if items, err = s.Repo.ItemsFor(ctx, m.DatabaseID); err != nil {
			return MigrationResponse{}, err
		}
	}
	rev, err := s.Repo.GetRevision(ctx, m.ID, m.CurrentRevision)
	if err != nil {
		return MigrationResponse{}, err
	}
	le, err := s.Repo.LatestExecution(ctx, m.ID)
	if err != nil {
		return MigrationResponse{}, err
	}
	resp := MigrationResponse{ID: m.ID, DatabaseID: m.DatabaseID, Title: m.Title, LinearOrder: m.LinearOrder,
		CurrentRevision: m.CurrentRevision, CreatedBy: m.CreatedBy, CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
		Revision: ToRevisionResponse(rev)}
	if le != nil {
		x := ToExecutionResponse(*le)
		resp.LatestExecution = &x
	}
	if s.check(ctx, uid, m.DatabaseID, true) != nil {
		return resp, nil
	}
	var self Item
	for _, it := range items {
		if it.ID == m.ID {
			self = it
		}
	}
	open := le != nil && (le.Status.Open() || le.Status.HoldsDatabase())
	started := le != nil && le.Status != StatusPendingReview && le.Status != StatusApproved &&
		le.Status != StatusRejected && le.Status != StatusCancelled
	resp.CanEdit = !started
	resp.CanSubmit = !open && !self.EverRun
	resp.CanForward = !open && self.EverRun && !self.Active
	if !open && self.Active {
		if last, ok := LastActive(items); ok && last.ID == m.ID {
			resp.CanRollback = true // reversibilitas dicek saat permintaan
		}
	}
	resp.CanQueue = le != nil && (le.Status == StatusApproved || le.Status == StatusFailed)
	return resp, nil
}

// RequestExecution: submit (up/forward otomatis) atau rollback.
func (s *Service) Submit(ctx context.Context, uid, dbID, migID int64) (ExecutionResponse, error) {
	items, err := s.Repo.ItemsFor(ctx, dbID)
	if err != nil {
		return ExecutionResponse{}, err
	}
	kind := KindUp
	for _, it := range items {
		if it.ID == migID && it.EverRun {
			kind = KindForward
		}
	}
	return s.request(ctx, uid, dbID, migID, kind)
}

func (s *Service) Rollback(ctx context.Context, uid, dbID, migID int64) (ExecutionResponse, error) {
	return s.request(ctx, uid, dbID, migID, KindRollback)
}

func (s *Service) request(ctx context.Context, uid, dbID, migID int64, kind Kind) (ExecutionResponse, error) {
	if err := s.check(ctx, uid, dbID, true); err != nil {
		return ExecutionResponse{}, err
	}
	e, err := s.Repo.RequestExecution(ctx, dbID, migID, uid, kind)
	if err != nil {
		return ExecutionResponse{}, err
	}
	if err := s.Approvals.Open(ctx, e.ID, dbID); err != nil {
		_, _ = s.Repo.SetStatus(ctx, e.ID, StatusCancelled, "gagal membuka approval")
		return ExecutionResponse{}, fmt.Errorf("membuka approval: %w", err)
	}
	s.Log.InfoContext(ctx, "execution.requested", "event", "execution.requested", "actor", uid, "database", dbID,
		"migration", migID, "execution", e.ID, "kind", string(kind), "status", string(e.Status))
	return ToExecutionResponse(e), nil
}

// MarkApproved dipanggil modul approval setelah semua leader menyetujui.
// Asumsi tim: eksekusi yang sudah disetujui langsung masuk antrean (alur spesifikasi:
// Approved -> Pending (Queue) -> Running). Idempoten dan dapat dipanggil ulang bila
// publikasi job sempat gagal; endpoint "queue" tetap tersedia sebagai cadangan manual.
func (s *Service) MarkApproved(ctx context.Context, execID int64) error {
	e, err := s.Repo.GetExecution(ctx, execID)
	if err != nil {
		return err
	}
	if e.Status == StatusPendingReview {
		if e, err = s.Repo.SetStatus(ctx, execID, StatusApproved, ""); err != nil {
			return err
		}
	}
	switch {
	case e.Status == StatusApproved:
		if e, err = s.Repo.SetStatus(ctx, execID, StatusQueued, ""); err != nil {
			return err
		}
		fallthrough
	case e.Status == StatusQueued && e.JobID == "":
		if _, err := s.publish(ctx, e, 0); err != nil {
			s.Log.Warn("approval.enqueue_failed", "event", "approval.enqueue_failed", "execution", execID, "error", err.Error())
			return err // status tetap queued tanpa job; panggil ulang atau pakai endpoint queue
		}
	}
	return nil
}

// MarkRejected dipanggil modul approval saat ada leader yang menolak.
func (s *Service) MarkRejected(ctx context.Context, execID int64, reason string) error {
	_, err := s.Repo.SetStatus(ctx, execID, StatusRejected, reason)
	return err
}

func (s *Service) Execution(ctx context.Context, uid, dbID, execID int64) (ExecutionResponse, error) {
	if err := s.check(ctx, uid, dbID, false); err != nil {
		return ExecutionResponse{}, err
	}
	e, err := s.Repo.GetExecutionScoped(ctx, dbID, execID)
	if err != nil {
		return ExecutionResponse{}, err
	}
	return ToExecutionResponse(e), nil
}

func (s *Service) Executions(ctx context.Context, uid, dbID int64) ([]ExecutionResponse, error) {
	if err := s.check(ctx, uid, dbID, false); err != nil {
		return nil, err
	}
	es, err := s.Repo.ListExecutions(ctx, dbID)
	if err != nil {
		return nil, err
	}
	out := make([]ExecutionResponse, 0, len(es))
	for _, e := range es {
		out = append(out, ToExecutionResponse(e))
	}
	return out, nil
}

// Queue: approved (atau failed → retry) → queued, lalu publikasi job. Mengembalikan job ID.
func (s *Service) Enqueue(ctx context.Context, uid, dbID, execID int64) (ExecuteResponse, error) {
	if err := s.check(ctx, uid, dbID, true); err != nil {
		return ExecuteResponse{}, err
	}
	e, err := s.Repo.GetExecutionScoped(ctx, dbID, execID)
	if err != nil {
		return ExecuteResponse{}, err
	}
	switch e.Status {
	case StatusApproved, StatusFailed:
		ok, err := s.Approvals.Satisfied(ctx, e.ID)
		if err != nil {
			return ExecuteResponse{}, err
		}
		if !ok {
			return ExecuteResponse{}, fmt.Errorf("%w: approval belum lengkap", ErrNotAllowed)
		}
		if e, err = s.Repo.SetStatus(ctx, e.ID, StatusQueued, ""); err != nil {
			return ExecuteResponse{}, err
		}
	case StatusQueued:
		if e.JobID != "" {
			return ExecuteResponse{ExecutionID: e.ID, JobID: e.JobID, Status: e.Status}, nil // idempoten
		}
	case StatusPartial, StatusUncertain, StatusPartiallyReverted:
		// resume: status tetap, handler melanjutkan dari checkpoint/marker
	default:
		return ExecuteResponse{}, fmt.Errorf("%w: status %s", ErrNotAllowed, e.Status)
	}
	return s.publish(ctx, e, 0)
}

func (s *Service) publish(ctx context.Context, e Execution, delay time.Duration) (ExecuteResponse, error) {
	jobID, err := s.Queue.Enqueue(ctx, e.ID, delay)
	if err != nil {
		return ExecuteResponse{}, fmt.Errorf("publikasi job: %w", err)
	}
	if err := s.Repo.SetJobID(ctx, e.ID, jobID); err != nil {
		return ExecuteResponse{}, err
	}
	s.Log.InfoContext(ctx, "execution.enqueued", "event", "execution.enqueued", "database", e.DatabaseID,
		"migration", e.MigrationID, "execution", e.ID, "job", jobID)
	return ExecuteResponse{ExecutionID: e.ID, JobID: jobID, Status: e.Status}, nil
}

// RevertBatches: batalkan batch yang sudah committed (kompensasi) untuk eksekusi partial/uncertain.
func (s *Service) RevertBatches(ctx context.Context, uid, dbID, execID int64) (ExecuteResponse, error) {
	if err := s.check(ctx, uid, dbID, true); err != nil {
		return ExecuteResponse{}, err
	}
	e, err := s.Repo.GetExecutionScoped(ctx, dbID, execID)
	if err != nil {
		return ExecuteResponse{}, err
	}
	rev, err := s.Repo.GetRevision(ctx, e.MigrationID, e.RevisionNumber)
	if err != nil {
		return ExecuteResponse{}, err
	}
	if !rev.IsBatchMode || e.Kind == KindRollback {
		return ExecuteResponse{}, fmt.Errorf("%w: hanya untuk migration batch", ErrNotAllowed)
	}
	if e.Status != StatusPartiallyReverted {
		if e, err = s.Repo.SetStatus(ctx, e.ID, StatusPartiallyReverted, ""); err != nil {
			return ExecuteResponse{}, err
		}
	}
	return s.publish(ctx, e, 0)
}

func (s *Service) Cancel(ctx context.Context, uid, dbID, execID int64) (ExecutionResponse, error) {
	if err := s.check(ctx, uid, dbID, true); err != nil {
		return ExecutionResponse{}, err
	}
	if _, err := s.Repo.GetExecutionScoped(ctx, dbID, execID); err != nil {
		return ExecutionResponse{}, err
	}
	e, err := s.Repo.SetStatus(ctx, execID, StatusCancelled, "dibatalkan pengguna")
	if err != nil {
		return ExecutionResponse{}, err
	}
	s.Log.InfoContext(ctx, "execution.cancelled", "event", "execution.cancelled", "actor", uid, "execution", execID)
	return ToExecutionResponse(e), nil
}

func (s *Service) History(ctx context.Context, uid, dbID int64, hideRollback bool) ([]HistoryItemResponse, error) {
	if err := s.check(ctx, uid, dbID, false); err != nil {
		return nil, err
	}
	hs, err := s.Repo.ListHistory(ctx, dbID)
	if err != nil {
		return nil, err
	}
	if hideRollback {
		hs = HideRollback(hs)
	}
	out := make([]HistoryItemResponse, 0, len(hs))
	for _, h := range hs {
		out = append(out, ToHistoryItem(h))
	}
	return out, nil
}
