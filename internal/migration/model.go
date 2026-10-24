package migration

import "time"

type Status string

const (
	StatusDraft   Status = "draft"
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

type Migration struct {
	ID             int64
	DBID           int64
	Version        *int
	Title          string
	SQL            string
	Status         Status
	CreatedBy      int64
	ExecutedBy     *int64
	CreatedByName  string
	ExecutedByName string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	ErrorMessage   string
}
