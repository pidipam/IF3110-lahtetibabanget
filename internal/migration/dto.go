package migration

import "time"

// CreateMigrationRequest DTO untuk input pembuatan migrasi baru (Draft)
type CreateMigrationRequest struct {
	Title              string  `json:"title"`
	SQLUp              string  `json:"sql_up"`
	SQLDown            *string `json:"sql_down,omitempty"`
	IsIrreversible     bool    `json:"is_irreversible"`
	IrreversibleReason *string `json:"irreversible_reason,omitempty"`
	IsBatchMode        bool    `json:"is_batch_mode"`
	BatchColumn        *string `json:"batch_column,omitempty"`
	BatchSize          *int    `json:"batch_size,omitempty"`
}

// MigrationResponse DTO respons detail migrasi beserta statusnya
type MigrationResponse struct {
	ID                 string    `json:"id"`
	DatabaseID         string    `json:"database_id"`
	Title              string    `json:"title"`
	LinearOrder        int       `json:"linear_order"`
	Status             string    `json:"status"` // Draft, Pending Review, Approved, Pending Queue, Running, Success, Failed
	CurrentRevision    Revision  `json:"current_revision"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Revision struct {
	ID                 string    `json:"id"`
	RevisionNumber     int       `json:"revision_number"`
	SQLUp              string    `json:"sql_up"`
	SQLDown            *string   `json:"sql_down,omitempty"`
	IsIrreversible     bool      `json:"is_irreversible"`
	IrreversibleReason *string   `json:"irreversible_reason,omitempty"`
	IsBatchMode        bool      `json:"is_batch_mode"`
	BatchColumn        *string   `json:"batch_column,omitempty"`
	BatchSize          *int      `json:"batch_size,omitempty"`
	Checksum           string    `json:"checksum"`
	CreatedAt          time.Time `json:"created_at"`
}

// ExecuteResponse DTO respons saat menekan tombol Jalankan/Rollback/Forward (Status HTTP 202 Accepted)
type ExecuteResponse struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

// HistoryItemResponse DTO untuk riwayat eksekusi (dengan fitur Hide Rollback)
type HistoryItemResponse struct {
	ExecutionID         string    `json:"execution_id"`
	MigrationID         string    `json:"migration_id"`
	RevisionID          string    `json:"revision_id"`
	ExecutionType       string    `json:"execution_type"` // APPLY, ROLLBACK, FORWARD
	ExecutedBy          string    `json:"executed_by"`
	ExecutedAt          time.Time `json:"executed_at"`
	SQLExecuted         string    `json:"sql_executed"`
	RevertedExecutionID *string   `json:"reverted_execution_id,omitempty"`
}