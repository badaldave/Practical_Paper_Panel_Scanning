package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PageVerification records whether a single page of a document has been marked
// reviewed by a verifier. "Submit" is gated on all pages being verified.
type PageVerification struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	DocumentID uuid.UUID  `json:"document_id"`
	PageNumber int        `json:"page_number"`
	IsVerified bool       `json:"is_verified"`
	VerifiedBy *uuid.UUID `json:"verified_by,omitempty"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

// VerificationEvent is an append-only activity record powering the live feed
// and all time-bucketed statistics.
type VerificationEvent struct {
	ID         uuid.UUID              `json:"id"`
	TenantID   uuid.UUID              `json:"tenant_id"`
	DocumentID *uuid.UUID             `json:"document_id,omitempty"`
	PageNumber *int                   `json:"page_number,omitempty"`
	UserID     *uuid.UUID             `json:"user_id,omitempty"`
	EventType  string                 `json:"event_type"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	// Joined display fields (optional)
	UserName     string `json:"user_name,omitempty"`
	DocumentName string `json:"document_name,omitempty"`
}

// VerificationItem is the pool/queue + per-document verification state, joined
// with assignee/locker display names and page-progress counts.
type VerificationItem struct {
	DocumentID         uuid.UUID  `json:"document_id"`
	Name               string     `json:"name"`
	Status             string     `json:"status"`              // document processing status
	VerificationStatus string     `json:"verification_status"` // pending|in_progress|submitted
	TotalPages         int        `json:"total_pages"`
	VerifiedPages      int        `json:"verified_pages"`
	LockedBy           *uuid.UUID `json:"locked_by,omitempty"`
	LockedByName       string     `json:"locked_by_name,omitempty"`
	LockedAt           *time.Time `json:"locked_at,omitempty"`
	AssignedTo         *uuid.UUID `json:"assigned_to,omitempty"`
	AssignedToName     string     `json:"assigned_to_name,omitempty"`
	CurrentPage        *int       `json:"current_page,omitempty"`
	LastActivityAt     *time.Time `json:"last_activity_at,omitempty"`
	SubmittedBy        *uuid.UUID `json:"submitted_by,omitempty"`
	SubmittedByName    string     `json:"submitted_by_name,omitempty"`
	SubmittedAt        *time.Time `json:"submitted_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

// VerificationRepository drives file distribution, locking, page progress and
// submission. All methods are tenant-scoped via the request context.
type VerificationRepository interface {
	// ListQueue returns verification items filtered by scope:
	//   "available" — pending & unlocked & claimable by userID
	//   "mine"      — locked by userID (in progress)
	//   "all"       — every document (admin view)
	//   "submitted" — completed
	ListQueue(ctx context.Context, scope string, userID uuid.UUID) ([]*VerificationItem, error)
	GetState(ctx context.Context, docID uuid.UUID) (*VerificationItem, error)

	// Claim atomically locks an unlocked, claimable document to userID.
	// Returns false if it was already locked / not claimable.
	Claim(ctx context.Context, docID, userID uuid.UUID) (bool, error)
	// Release clears the lock. When force is true (admin) it ignores ownership.
	Release(ctx context.Context, docID, userID uuid.UUID, force bool) error
	// Assign pins/unpins a document to a specific verifier (admin). nil unpins.
	Assign(ctx context.Context, docID uuid.UUID, assignee *uuid.UUID) error
	// SetPresence updates which page the locking user is currently viewing.
	SetPresence(ctx context.Context, docID, userID uuid.UUID, page int) error

	GetPageVerifications(ctx context.Context, docID uuid.UUID) ([]*PageVerification, error)
	MarkPage(ctx context.Context, docID uuid.UUID, page int, userID uuid.UUID, verified bool) error
	// Submit marks the whole document verified. Returns an error if the caller
	// does not hold the lock or not all pages are verified.
	Submit(ctx context.Context, docID, userID uuid.UUID) error

	LogEvent(ctx context.Context, ev *VerificationEvent) error
}
