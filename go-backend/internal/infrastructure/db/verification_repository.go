package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

type VerificationRepository struct {
	db *DB
}

func NewVerificationRepository(db *DB) *VerificationRepository {
	return &VerificationRepository{db: db}
}

// itemSelect is the shared projection for a VerificationItem joined with the
// display names of the locking / assigned / submitting users and page counts.
const itemSelect = `
	SELECT d.id, d.name, d.status, d.verification_status,
		(SELECT COUNT(*) FROM document_pages dp WHERE dp.document_id = d.id) AS total_pages,
		(SELECT COUNT(*) FROM page_verifications pv WHERE pv.document_id = d.id AND pv.is_verified) AS verified_pages,
		d.locked_by, TRIM(COALESCE(lb.first_name,'') || ' ' || COALESCE(lb.last_name,'')),
		d.locked_at,
		d.assigned_to, TRIM(COALESCE(ab.first_name,'') || ' ' || COALESCE(ab.last_name,'')),
		d.current_page, d.last_activity_at,
		d.submitted_by, TRIM(COALESCE(sb.first_name,'') || ' ' || COALESCE(sb.last_name,'')),
		d.submitted_at, d.created_at
	FROM documents d
	LEFT JOIN users lb ON lb.id = d.locked_by
	LEFT JOIN users ab ON ab.id = d.assigned_to
	LEFT JOIN users sb ON sb.id = d.submitted_by`

func scanItem(scan func(dest ...interface{}) error) (*domain.VerificationItem, error) {
	var it domain.VerificationItem
	var lockedBy, assignedTo, submittedBy uuid.NullUUID
	var lockedName, assignedName, submittedName sql.NullString
	var lockedAt, lastActivity, submittedAt sql.NullTime
	var currentPage sql.NullInt32
	if err := scan(
		&it.DocumentID, &it.Name, &it.Status, &it.VerificationStatus,
		&it.TotalPages, &it.VerifiedPages,
		&lockedBy, &lockedName, &lockedAt,
		&assignedTo, &assignedName,
		&currentPage, &lastActivity,
		&submittedBy, &submittedName, &submittedAt, &it.CreatedAt,
	); err != nil {
		return nil, err
	}
	if lockedBy.Valid {
		id := lockedBy.UUID
		it.LockedBy = &id
		it.LockedByName = lockedName.String
	}
	if assignedTo.Valid {
		id := assignedTo.UUID
		it.AssignedTo = &id
		it.AssignedToName = assignedName.String
	}
	if submittedBy.Valid {
		id := submittedBy.UUID
		it.SubmittedBy = &id
		it.SubmittedByName = submittedName.String
	}
	if lockedAt.Valid {
		it.LockedAt = &lockedAt.Time
	}
	if lastActivity.Valid {
		it.LastActivityAt = &lastActivity.Time
	}
	if submittedAt.Valid {
		it.SubmittedAt = &submittedAt.Time
	}
	if currentPage.Valid {
		p := int(currentPage.Int32)
		it.CurrentPage = &p
	}
	return &it, nil
}

func (r *VerificationRepository) ListQueue(ctx context.Context, scope string, userID uuid.UUID) ([]*domain.VerificationItem, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}

	where := " WHERE d.tenant_id = $1"
	args := []interface{}{tenantID}
	order := " ORDER BY d.created_at DESC"

	switch scope {
	case "available":
		where += ` AND d.status = 'extracted' AND d.verification_status = 'pending'
			AND d.locked_by IS NULL AND (d.assigned_to IS NULL OR d.assigned_to = $2)`
		args = append(args, userID)
		order = " ORDER BY d.assigned_to NULLS LAST, d.created_at ASC" // pinned + oldest first
	case "mine":
		where += " AND d.locked_by = $2 AND d.verification_status = 'in_progress'"
		args = append(args, userID)
		order = " ORDER BY d.locked_at ASC"
	case "submitted":
		where += " AND d.verification_status = 'submitted'"
		order = " ORDER BY d.submitted_at DESC"
	case "all":
		// no extra filter (admin oversight)
	default:
		return nil, fmt.Errorf("unknown queue scope: %s", scope)
	}

	rows, err := r.db.QueryContext(ctx, itemSelect+where+order, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list verification queue: %w", err)
	}
	defer rows.Close()

	var items []*domain.VerificationItem
	for rows.Next() {
		it, err := scanItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

func (r *VerificationRepository) GetState(ctx context.Context, docID uuid.UUID) (*domain.VerificationItem, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}
	it, err := scanItem(r.db.QueryRowContext(ctx, itemSelect+" WHERE d.id = $1 AND d.tenant_id = $2", docID, tenantID).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get verification state: %w", err)
	}
	return it, nil
}

// Claim atomically locks an unlocked, claimable document and resets page
// progress (per the "start from start" takeover rule — prior cell edits remain
// in history, only the per-page review checklist is cleared).
//
// A document that was already submitted can be claimed again — files may be
// verified more than once, and each new submission simply overwrites the
// document's status with whatever the latest pass produces.
func (r *VerificationRepository) Claim(ctx context.Context, docID, userID uuid.UUID) (bool, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return false, errors.New("tenant context required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var claimedID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		UPDATE documents
		SET locked_by = $2, locked_at = now(), verification_status = 'in_progress',
			verification_started_at = COALESCE(verification_started_at, now()),
			current_page = 1, last_activity_at = now(), updated_at = now()
		WHERE id = $1 AND tenant_id = $3
		  AND status IN ('extracted', 'verified')
		  AND locked_by IS NULL
		  AND (assigned_to IS NULL OR assigned_to = $2)
		RETURNING id
	`, docID, userID, tenantID).Scan(&claimedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil // already locked or not claimable
		}
		return false, fmt.Errorf("failed to claim document: %w", err)
	}

	// Reset the per-page review checklist for a fresh pass.
	if _, err := tx.ExecContext(ctx, "DELETE FROM page_verifications WHERE document_id = $1", docID); err != nil {
		return false, fmt.Errorf("failed to reset page progress: %w", err)
	}

	if err := logEventTx(ctx, tx, tenantID, &docID, nil, &userID, "claim", nil); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *VerificationRepository) Release(ctx context.Context, docID, userID uuid.UUID, force bool) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required")
	}

	query := `
		UPDATE documents
		SET locked_by = NULL, locked_at = NULL, current_page = NULL,
			verification_status = CASE WHEN verification_status = 'submitted' THEN 'submitted' ELSE 'pending' END,
			last_activity_at = now(), updated_at = now()
		WHERE id = $1 AND tenant_id = $2`
	args := []interface{}{docID, tenantID}
	if !force {
		query += " AND locked_by = $3"
		args = append(args, userID)
	} else {
		query += " AND locked_by IS NOT NULL"
	}

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to release document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("you do not hold the lock on this document")
	}

	evType := "release"
	if force {
		evType = "force_release"
	}
	return r.LogEvent(ctx, &domain.VerificationEvent{TenantID: tenantID, DocumentID: &docID, UserID: &userID, EventType: evType})
}

func (r *VerificationRepository) Assign(ctx context.Context, docID uuid.UUID, assignee *uuid.UUID) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required")
	}
	res, err := r.db.ExecContext(ctx,
		"UPDATE documents SET assigned_to = $2, updated_at = now() WHERE id = $1 AND tenant_id = $3",
		docID, assignee, tenantID)
	if err != nil {
		return fmt.Errorf("failed to assign document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("document not found")
	}
	evType := "assign"
	if assignee == nil {
		evType = "unassign"
	}
	actor, _ := contextutil.GetUserID(ctx)
	meta := map[string]interface{}{}
	if assignee != nil {
		meta["assignee"] = assignee.String()
	}
	return r.LogEvent(ctx, &domain.VerificationEvent{TenantID: tenantID, DocumentID: &docID, UserID: &actor, EventType: evType, Metadata: meta})
}

func (r *VerificationRepository) SetPresence(ctx context.Context, docID, userID uuid.UUID, page int) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required")
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE documents SET current_page = $3, last_activity_at = now(), updated_at = now()
		 WHERE id = $1 AND tenant_id = $4 AND locked_by = $2`,
		docID, userID, page, tenantID)
	if err != nil {
		return fmt.Errorf("failed to update presence: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("you do not hold the lock on this document")
	}
	return nil
}

func (r *VerificationRepository) GetPageVerifications(ctx context.Context, docID uuid.UUID) ([]*domain.PageVerification, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, tenant_id, document_id, page_number, is_verified, verified_by, verified_at
		 FROM page_verifications WHERE document_id = $1 ORDER BY page_number ASC`, docID)
	if err != nil {
		return nil, fmt.Errorf("failed to get page verifications: %w", err)
	}
	defer rows.Close()

	var out []*domain.PageVerification
	for rows.Next() {
		var pv domain.PageVerification
		var verifiedBy uuid.NullUUID
		var verifiedAt sql.NullTime
		if err := rows.Scan(&pv.ID, &pv.TenantID, &pv.DocumentID, &pv.PageNumber, &pv.IsVerified, &verifiedBy, &verifiedAt); err != nil {
			return nil, err
		}
		if verifiedBy.Valid {
			id := verifiedBy.UUID
			pv.VerifiedBy = &id
		}
		if verifiedAt.Valid {
			pv.VerifiedAt = &verifiedAt.Time
		}
		out = append(out, &pv)
	}
	return out, nil
}

func (r *VerificationRepository) MarkPage(ctx context.Context, docID uuid.UUID, page int, userID uuid.UUID, verified bool) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required")
	}

	// Caller must hold the lock to change the review checklist.
	var locked bool
	if err := r.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2 AND locked_by = $3)",
		docID, tenantID, userID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("you do not hold the lock on this document")
	}

	var verifiedBy *uuid.UUID
	var verifiedAt *time.Time
	if verified {
		verifiedBy = &userID
		now := time.Now()
		verifiedAt = &now
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO page_verifications (id, tenant_id, document_id, page_number, is_verified, verified_by, verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		ON CONFLICT (document_id, page_number) DO UPDATE
		SET is_verified = EXCLUDED.is_verified, verified_by = EXCLUDED.verified_by,
			verified_at = EXCLUDED.verified_at, updated_at = now()
	`, uuid.New(), tenantID, docID, page, verified, verifiedBy, verifiedAt)
	if err != nil {
		return fmt.Errorf("failed to mark page: %w", err)
	}

	evType := "page_verified"
	if !verified {
		evType = "page_unverified"
	}
	return r.LogEvent(ctx, &domain.VerificationEvent{TenantID: tenantID, DocumentID: &docID, PageNumber: &page, UserID: &userID, EventType: evType})
}

func (r *VerificationRepository) Submit(ctx context.Context, docID, userID uuid.UUID) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Must hold the lock.
	var locked bool
	if err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2 AND locked_by = $3)",
		docID, tenantID, userID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("you do not hold the lock on this document")
	}

	// Gate: every page must be verified.
	var totalPages, verifiedPages int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM document_pages WHERE document_id = $1", docID).Scan(&totalPages); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM page_verifications WHERE document_id = $1 AND is_verified", docID).Scan(&verifiedPages); err != nil {
		return err
	}
	if totalPages == 0 {
		return errors.New("document has no pages to verify")
	}
	if verifiedPages < totalPages {
		return fmt.Errorf("cannot submit: %d of %d pages verified", verifiedPages, totalPages)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE documents
		SET verification_status = 'submitted', status = 'verified',
			submitted_by = $2, submitted_at = now(),
			locked_by = NULL, locked_at = NULL, current_page = NULL,
			last_activity_at = now(), updated_at = now()
		WHERE id = $1 AND tenant_id = $3
	`, docID, userID, tenantID); err != nil {
		return fmt.Errorf("failed to submit document: %w", err)
	}

	if err := logEventTx(ctx, tx, tenantID, &docID, nil, &userID, "submit", map[string]interface{}{"pages": totalPages}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *VerificationRepository) LogEvent(ctx context.Context, ev *domain.VerificationEvent) error {
	metaJSON, _ := json.Marshal(ev.Metadata)
	if ev.Metadata == nil {
		metaJSON = []byte("{}")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO verification_events (id, tenant_id, document_id, page_number, user_id, event_type, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
	`, uuid.New(), ev.TenantID, ev.DocumentID, ev.PageNumber, ev.UserID, ev.EventType, metaJSON)
	if err != nil {
		return fmt.Errorf("failed to log verification event: %w", err)
	}
	return nil
}

// logEventTx writes an event within an existing transaction.
func logEventTx(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, docID *uuid.UUID, page *int, userID *uuid.UUID, evType string, meta map[string]interface{}) error {
	metaJSON := []byte("{}")
	if meta != nil {
		metaJSON, _ = json.Marshal(meta)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO verification_events (id, tenant_id, document_id, page_number, user_id, event_type, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
	`, uuid.New(), tenantID, docID, page, userID, evType, metaJSON)
	return err
}
