package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Overview is the top-of-dashboard snapshot of platform activity.
type Overview struct {
	TotalDocuments     int `json:"total_documents"`
	PendingVerification int `json:"pending_verification"`
	InProgress         int `json:"in_progress"`
	Submitted          int `json:"submitted"`
	FailedDocuments    int `json:"failed_documents"`
	TotalPages         int `json:"total_pages"`
	VerifiedPages      int `json:"verified_pages"`

	FilesSubmittedToday int `json:"files_submitted_today"`
	PagesVerifiedToday  int `json:"pages_verified_today"`
	FilesSubmittedMonth int `json:"files_submitted_month"`
	PagesVerifiedMonth  int `json:"pages_verified_month"`

	ActiveUsers   int `json:"active_users"`   // users with a live lock right now
	TotalUsers    int `json:"total_users"`
	CellEditsToday int `json:"cell_edits_today"`
}

// PresenceRow is one verifier currently holding a file open (live monitoring).
type PresenceRow struct {
	UserID         uuid.UUID  `json:"user_id"`
	UserName       string     `json:"user_name"`
	Email          string     `json:"email"`
	DocumentID     uuid.UUID  `json:"document_id"`
	DocumentName   string     `json:"document_name"`
	CurrentPage    *int       `json:"current_page,omitempty"`
	TotalPages     int        `json:"total_pages"`
	VerifiedPages  int        `json:"verified_pages"`
	LockedAt       *time.Time `json:"locked_at,omitempty"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
}

// ProductivityRow is per-user throughput over a date range.
type ProductivityRow struct {
	UserID         uuid.UUID `json:"user_id"`
	UserName       string    `json:"user_name"`
	Email          string    `json:"email"`
	PagesVerified  int       `json:"pages_verified"`
	FilesSubmitted int       `json:"files_submitted"`
	CellsEdited    int       `json:"cells_edited"`
}

// TimeseriesPoint is a per-day bucket of throughput for charts.
type TimeseriesPoint struct {
	Day            string `json:"day"` // YYYY-MM-DD
	PagesVerified  int    `json:"pages_verified"`
	FilesSubmitted int    `json:"files_submitted"`
	CellsEdited    int    `json:"cells_edited"`
}

// StatsRepository serves the analytics surface. All queries are tenant-scoped.
type StatsRepository interface {
	Overview(ctx context.Context) (*Overview, error)
	Presence(ctx context.Context) ([]*PresenceRow, error)
	Productivity(ctx context.Context, from, to time.Time) ([]*ProductivityRow, error)
	Timeseries(ctx context.Context, from, to time.Time) ([]*TimeseriesPoint, error)
	RecentActivity(ctx context.Context, limit int) ([]*VerificationEvent, error)
}
