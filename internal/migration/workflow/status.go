// Package workflow berisi model, aturan, dan API alur migration M2
// (draft -> review -> approval -> queue -> eksekusi -> history).
package workflow

import "errors"

// Kind adalah jenis eksekusi.
type Kind string

const (
	KindUp       Kind = "up"
	KindRollback Kind = "rollback"
	KindForward  Kind = "forward"
)

func (k Kind) Valid() bool { return k == KindUp || k == KindRollback || k == KindForward }

// Status adalah status sebuah eksekusi.
type Status string

const (
	StatusPendingReview     Status = "pending_review"
	StatusApproved          Status = "approved"
	StatusQueued            Status = "queued"
	StatusRunning           Status = "running"
	StatusSuccess           Status = "success"
	StatusFailed            Status = "failed"
	StatusPartial           Status = "partial"
	StatusUncertain         Status = "uncertain"
	StatusPartiallyReverted Status = "partially_reverted"
	StatusRejected          Status = "rejected"
	StatusCancelled         Status = "cancelled"
)

var ErrInvalidTransition = errors.New("transisi status tidak valid")

var transitions = map[Status][]Status{
	StatusPendingReview: {StatusApproved, StatusRejected, StatusCancelled},
	StatusApproved:      {StatusQueued, StatusCancelled},
	StatusQueued:        {StatusRunning, StatusCancelled},
	StatusRunning:       {StatusSuccess, StatusFailed, StatusPartial, StatusUncertain, StatusPartiallyReverted, StatusQueued, StatusCancelled},
	StatusFailed:        {StatusQueued},
	StatusPartial:       {StatusRunning, StatusPartiallyReverted},
	StatusUncertain:     {StatusRunning, StatusPartiallyReverted},
	// partially_reverted dilanjutkan lagi sebagai pembatalan batch.
	StatusPartiallyReverted: {StatusRunning},
}

// CanTransition melaporkan apakah from -> to diizinkan.
func CanTransition(from, to Status) error {
	for _, t := range transitions[from] {
		if t == to {
			return nil
		}
	}
	return ErrInvalidTransition
}

// Terminal: tidak akan berubah lagi.
func (s Status) Terminal() bool {
	return s == StatusSuccess || s == StatusRejected || s == StatusCancelled
}

// HoldsDatabase: status yang menahan database (migrasi berikutnya harus menunggu).
func (s Status) HoldsDatabase() bool {
	switch s {
	case StatusRunning, StatusPartial, StatusUncertain, StatusPartiallyReverted:
		return true
	}
	return false
}

// Open: eksekusi yang masih "hidup" untuk sebuah migration.
func (s Status) Open() bool { return !s.Terminal() && s != StatusFailed }
