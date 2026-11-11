package approval

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

const MaxReasonLength = 500

var (
	ErrNotFound = errors.New("migration tidak ditemukan")
	ErrNoPending = errors.New("tidak ada permintaan yang menunggu persetujuan")
	ErrRevisionMismatch = errors.New("revisi sudah berubah, muat ulang halaman sebelum memutuskan")
	ErrNotApprover = errors.New("Anda bukan leader grup yang wajib menyetujui permintaan ini")
	ErrSelfApproval = errors.New("pengaju tidak boleh memutuskan permintaannya sendiri")
	ErrAlreadyDecided = errors.New("Grup Anda sudah memberi keputusan untuk revisi ini")
	ErrReasonRequired = errors.New("alasan penolakan wajib diisi")
	ErrReasonTooLong = errors.New("alasan penolakan maksimal 500 karakter")
	ErrApprovalStale = errors.New("persetujuan tidak lagi berlaku")
	ErrNotReopenable = errors.New("permintaan tidak bisa dikembalikan ke peninjauan")
)

type StaleError struct {
	State State
	Message string
}

func (e *StaleError) Error() string {
	return ErrApprovalStale.Error() + ": " + e.Message
}

func (e *StaleError) Is(target error) bool {
	return target == ErrApprovalStale
}

type Approvers interface {
	ApproverGroups(ctx context.Context, p authz.Principal, dbID int64) ([]int64, error)
}

type Service struct {
	db *pgxpool.Pool
	authz Approvers
	OnApproved func(ctx context.Context, execID int64)
}

func NewService(db *pgxpool.Pool, az Approvers) *Service {
	return &Service{db: db, authz: az}
}

func (s *Service) RequiredApprovers(ctx context.Context, dbID int64) ([]Group, error) {
	return loadGroups(ctx, s.db, dbID)
}

func (s *Service) Status(ctx context.Context, execID int64) (Status, error) {
	exec, err := loadExecution(ctx, s.db, execID)
	if err != nil {
		return Status{}, err
	}
	return evaluateWith(ctx, s.db, exec)
}

func (s *Service) StatusForMigration(ctx context.Context, migrationID int64) (Status, error) {
	exec, err := latestExecution(ctx, s.db, migrationID)
	if errors.Is(err, ErrNotFound) {
		ok, e := migrationExists(ctx, s.db, migrationID)
		if e != nil {
			return Status{}, e
		}
		if ok {
			return Status{}, ErrNoPending
		}
	}
	if err != nil {
		return Status{}, err
	}
	return evaluateWith(ctx, s.db, exec)
}

func (s *Service) Check(ctx context.Context, execID int64) error {
	st, err := s.Status(ctx, execID)
	if err != nil {
		return err
	}
	if st.Execution.Status != "approved" && st.Execution.Status != "queued" {
		return &StaleError{State: st.State, Message: "Status permintaan " + st.Execution.Status + ", bukan approved."}
	}
	if st.State != StateApproved {
		return &StaleError{State: st.State, Message: st.Message}
	}
	return nil
}

func (s *Service) Reopen(ctx context.Context, execID int64) error {
	ok, err := setStatus(ctx, s.db, execID, []string{"approved", "queued"}, "pending_review", "")
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotReopenable
	}
	return nil
}

func (s *Service) Pending(ctx context.Context, p authz.Principal, mine bool, limit, offset int) ([]Status, int, error) {
	list, total, err := listPending(ctx, s.db, p.UserID, mine, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Status, 0, len(list))
	for i := range list {
		st, err := evaluateWith(ctx, s.db, &list[i])
		if err != nil {
			return nil, 0, err
		}
		out = append(out, st)
	}
	return out, total, nil
}

func (s *Service) Approve(ctx context.Context, p authz.Principal, migrationID int64, revision int) (Status, error) {
	return s.decide(ctx, p, migrationID, revision, DecisionApproved, "")
}

func (s *Service) Reject(ctx context.Context, p authz.Principal, migrationID int64, revision int, reason string) (Status, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Status{}, ErrReasonRequired
	}
	if len([]rune(reason)) > MaxReasonLength {
		return Status{}, ErrReasonTooLong
	}
	return s.decide(ctx, p, migrationID, revision, DecisionRejected, reason)
}

func (s *Service) decide(ctx context.Context, p authz.Principal, migrationID int64, revision int, d Decision, reason string) (Status, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Status{}, err
	}
	defer tx.Rollback(ctx)
	exec, err := lockPending(ctx, tx, migrationID)
	if errors.Is(err, ErrNotFound) {
		ok, e := migrationExists(ctx, tx, migrationID)
		if e != nil {
			return Status{}, e
		}
		if ok {
			return Status{}, ErrNoPending
		}
		return Status{}, ErrNotFound
	}
	if err != nil {
		return Status{}, err
	}
	mine, err := s.authz.ApproverGroups(ctx, p, exec.DatabaseID)
	if err != nil {
		return Status{}, err
	}
	if len(mine) == 0 {
		return Status{}, ErrNotApprover
	}
	if exec.RequestedBy == p.UserID {
		return Status{}, ErrSelfApproval
	}
	if revision != exec.RevisionNumber || !exec.RevisionCurrent() {
		return Status{}, ErrRevisionMismatch
	}
	before, err := evaluateWith(ctx, tx, exec)
	if err != nil {
		return Status{}, err
	}
	open := map[int64]bool{}
	for _, g := range before.Groups {
		if g.Decision == DecisionPending {
			open[g.ID] = true
		}
	}
	decided := 0
	for _, gid := range mine {
		if !open[gid] {
			continue
		}
		if err := insertVote(ctx, tx, exec, gid, p.UserID, d, reason); err != nil {
			return Status{}, err
		}
		decided++
	}
	if decided == 0 {
		return Status{}, ErrAlreadyDecided
	}
	after, err := evaluateWith(ctx, tx, exec)
	if err != nil {
		return Status{}, err
	}
	switch after.State {
	case StateApproved:
		_, err = setStatus(ctx, tx, exec.ID, []string{"pending_review"}, "approved", "")
		after.Execution.Status = "approved"
	case StateRejected:
		_, err = setStatus(ctx, tx, exec.ID, []string{"pending_review"}, "rejected", reason)
		after.Execution.Status = "rejected"
		after.Execution.RejectReason = reason
		after.Message = "Permintaan ditolak: " + reason
	}
	if err != nil {
		return Status{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Status{}, err
	}
	if after.State == StateApproved && s.OnApproved != nil {
		s.OnApproved(ctx, exec.ID)
	}
	return after, nil
}
