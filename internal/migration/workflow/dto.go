package workflow

import "time"

// Semua ID int64, status berupa nilai mesin (snake_case), waktu ISO 8601 UTC.

type CreateRequest struct {
	Title              string `json:"title"`
	SQLUp              string `json:"sql_up"`
	SQLDown            string `json:"sql_down"`
	IsIrreversible     bool   `json:"is_irreversible"`
	IrreversibleReason string `json:"irreversible_reason"`
	IsBatchMode        bool   `json:"is_batch_mode"`
	BatchColumn        string `json:"batch_column"`
	BatchSize          int    `json:"batch_size"`
}

// ReviseRequest membuat revisi baru (PATCH). Revisi lama tidak berubah.
type ReviseRequest = CreateRequest

type RevisionResponse struct {
	RevisionNumber     int       `json:"revision_number"`
	SQLUp              string    `json:"sql_up"`
	SQLDown            string    `json:"sql_down"`
	IsIrreversible     bool      `json:"is_irreversible"`
	IrreversibleReason string    `json:"irreversible_reason"`
	IsBatchMode        bool      `json:"is_batch_mode"`
	BatchColumn        string    `json:"batch_column"`
	BatchSize          int       `json:"batch_size"`
	Checksum           string    `json:"checksum"`
	CreatedBy          int64     `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
}

type ExecutionResponse struct {
	ID                int64      `json:"id"`
	MigrationID       int64      `json:"migration_id"`
	RevisionNumber    int        `json:"revision_number"`
	Kind              Kind       `json:"kind"`
	Status            Status     `json:"status"`
	UndoesExecutionID *int64     `json:"undoes_execution_id"`
	RequestedBy       int64      `json:"requested_by"`
	Attempt           int        `json:"attempt"`
	JobID             string     `json:"job_id"`
	RejectReason      string     `json:"reject_reason"`
	ErrorMessage      string     `json:"error_message"`
	BatchesCommitted  int        `json:"batches_committed"`
	RowsCommitted     int64      `json:"rows_committed"`
	CreatedAt         time.Time  `json:"created_at"`
	StartedAt         *time.Time `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
}

type MigrationResponse struct {
	ID              int64              `json:"id"`
	DatabaseID      int64              `json:"database_id"`
	Title           string             `json:"title"`
	LinearOrder     *int               `json:"linear_order"`
	CurrentRevision int                `json:"current_revision"`
	CreatedBy       int64              `json:"created_by"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Revision        RevisionResponse   `json:"revision"`
	LatestExecution *ExecutionResponse `json:"latest_execution"`
	CanEdit         bool               `json:"can_edit"`
	CanSubmit       bool               `json:"can_submit"`
	CanQueue        bool               `json:"can_queue"`
	CanRollback     bool               `json:"can_rollback"`
	CanForward      bool               `json:"can_forward"`
}

// ExecuteResponse dikembalikan dengan HTTP 202.
type ExecuteResponse struct {
	ExecutionID int64  `json:"execution_id"`
	JobID       string `json:"job_id"`
	Status      Status `json:"status"`
}

type HistoryItemResponse struct {
	ExecutionID         int64     `json:"execution_id"`
	MigrationID         int64     `json:"migration_id"`
	MigrationTitle      string    `json:"migration_title"`
	DatabaseID          int64     `json:"database_id"`
	RevisionNumber      int       `json:"revision_number"`
	ExecutionType       Kind      `json:"execution_type"`
	Status              Status    `json:"status"`
	ExecutedBy          int64     `json:"executed_by"`
	ExecutedAt          time.Time `json:"executed_at"`
	SQLExecuted         string    `json:"sql_executed"`
	RevertedExecutionID *int64    `json:"reverted_execution_id"`
}

type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

func ToRevisionResponse(r Revision) RevisionResponse {
	return RevisionResponse{
		RevisionNumber: r.Number, SQLUp: r.SQLUp, SQLDown: r.SQLDown,
		IsIrreversible: r.IsIrreversible, IrreversibleReason: r.IrreversibleReason,
		IsBatchMode: r.IsBatchMode, BatchColumn: r.BatchColumn, BatchSize: r.BatchSize,
		Checksum: r.Checksum, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt.UTC(),
	}
}

func ToExecutionResponse(e Execution) ExecutionResponse {
	return ExecutionResponse{
		ID: e.ID, MigrationID: e.MigrationID, RevisionNumber: e.RevisionNumber,
		Kind: e.Kind, Status: e.Status, UndoesExecutionID: e.UndoesExecution,
		RequestedBy: e.RequestedBy, Attempt: e.Attempt, JobID: e.JobID,
		RejectReason: e.RejectReason, ErrorMessage: e.ErrorMessage,
		BatchesCommitted: e.BatchesCommitted, RowsCommitted: e.RowsCommitted,
		CreatedAt: e.CreatedAt.UTC(), StartedAt: e.StartedAt, FinishedAt: e.FinishedAt,
	}
}

func ToHistoryItem(h HistoryEntry) HistoryItemResponse {
	return HistoryItemResponse{
		ExecutionID: h.ExecutionID, MigrationID: h.MigrationID, MigrationTitle: h.MigrationTitle,
		DatabaseID: h.DatabaseID, RevisionNumber: h.RevisionNumber, ExecutionType: h.Kind,
		Status: h.Status, ExecutedBy: h.ExecutedBy, ExecutedAt: h.ExecutedAt.UTC(),
		SQLExecuted: h.SQLExecuted, RevertedExecutionID: h.RevertedExecutionID,
	}
}
