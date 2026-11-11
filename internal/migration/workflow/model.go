package workflow

import "time"

type Migration struct {
	ID              int64
	DatabaseID      int64
	Title           string
	LinearOrder     *int // nil sampai diajukan
	CurrentRevision int
	CreatedBy       int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Revision struct {
	MigrationID        int64
	Number             int
	SQLUp              string
	SQLDown            string
	IsIrreversible     bool
	IrreversibleReason string
	IsBatchMode        bool
	BatchColumn        string
	BatchSize          int
	Checksum           string
	CreatedBy          int64
	CreatedAt          time.Time
}

type Execution struct {
	ID               int64
	MigrationID      int64
	DatabaseID       int64
	RevisionNumber   int
	Kind             Kind
	Status           Status
	UndoesExecution  *int64
	RequestedBy      int64
	ExecutedSQL      string
	Attempt          int
	JobID            string
	RejectReason     string
	ErrorMessage     string
	BatchesCommitted int
	RowsCommitted    int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	QueuedAt         *time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

// HistoryEntry adalah satu eksekusi sukses yang sudah di-commit.
type HistoryEntry struct {
	ExecutionID         int64
	MigrationID         int64
	MigrationTitle      string
	DatabaseID          int64
	RevisionNumber      int
	Kind                Kind
	Status              Status
	ExecutedBy          int64
	ExecutedAt          time.Time
	SQLExecuted         string
	RevertedExecutionID *int64
}
