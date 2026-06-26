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

type StatsRepository struct {
	db *DB
}

func NewStatsRepository(db *DB) *StatsRepository {
	return &StatsRepository{db: db}
}

func (r *StatsRepository) Overview(ctx context.Context) (*domain.Overview, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}

	var o domain.Overview
	err = r.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM documents WHERE tenant_id = $1),
			(SELECT COUNT(*) FROM documents WHERE tenant_id = $1 AND verification_status = 'pending'),
			(SELECT COUNT(*) FROM documents WHERE tenant_id = $1 AND verification_status = 'in_progress'),
			(SELECT COUNT(*) FROM documents WHERE tenant_id = $1 AND verification_status = 'submitted'),
			(SELECT COUNT(*) FROM documents WHERE tenant_id = $1 AND status = 'failed'),
			(SELECT COUNT(*) FROM document_pages dp JOIN documents d ON d.id = dp.document_id WHERE d.tenant_id = $1),
			(SELECT COUNT(*) FROM page_verifications WHERE tenant_id = $1 AND is_verified),
			(SELECT COUNT(*) FROM documents WHERE tenant_id = $1 AND submitted_at::date = CURRENT_DATE),
			(SELECT COUNT(*) FROM page_verifications WHERE tenant_id = $1 AND is_verified AND verified_at::date = CURRENT_DATE),
			(SELECT COUNT(*) FROM documents WHERE tenant_id = $1 AND submitted_at >= date_trunc('month', now())),
			(SELECT COUNT(*) FROM page_verifications WHERE tenant_id = $1 AND is_verified AND verified_at >= date_trunc('month', now())),
			(SELECT COUNT(DISTINCT locked_by) FROM documents WHERE tenant_id = $1 AND locked_by IS NOT NULL),
			(SELECT COUNT(*) FROM users WHERE tenant_id = $1),
			(SELECT COUNT(*) FROM extracted_cells_history h JOIN documents d ON d.id = h.document_id
				WHERE d.tenant_id = $1 AND h.version > 1 AND h.created_at::date = CURRENT_DATE)
	`, tenantID).Scan(
		&o.TotalDocuments, &o.PendingVerification, &o.InProgress, &o.Submitted, &o.FailedDocuments,
		&o.TotalPages, &o.VerifiedPages,
		&o.FilesSubmittedToday, &o.PagesVerifiedToday,
		&o.FilesSubmittedMonth, &o.PagesVerifiedMonth,
		&o.ActiveUsers, &o.TotalUsers, &o.CellEditsToday,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute overview: %w", err)
	}
	return &o, nil
}

func (r *StatsRepository) Presence(ctx context.Context) ([]*domain.PresenceRow, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.locked_by, TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), u.email,
			d.id, d.name, d.current_page,
			(SELECT COUNT(*) FROM document_pages dp WHERE dp.document_id = d.id),
			(SELECT COUNT(*) FROM page_verifications pv WHERE pv.document_id = d.id AND pv.is_verified),
			d.locked_at, d.last_activity_at
		FROM documents d
		JOIN users u ON u.id = d.locked_by
		WHERE d.tenant_id = $1 AND d.locked_by IS NOT NULL
		ORDER BY d.last_activity_at DESC NULLS LAST
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query presence: %w", err)
	}
	defer rows.Close()

	out := []*domain.PresenceRow{}
	for rows.Next() {
		var p domain.PresenceRow
		var currentPage sql.NullInt32
		var lockedAt, lastActivity sql.NullTime
		if err := rows.Scan(&p.UserID, &p.UserName, &p.Email, &p.DocumentID, &p.DocumentName,
			&currentPage, &p.TotalPages, &p.VerifiedPages, &lockedAt, &lastActivity); err != nil {
			return nil, err
		}
		if currentPage.Valid {
			v := int(currentPage.Int32)
			p.CurrentPage = &v
		}
		if lockedAt.Valid {
			p.LockedAt = &lockedAt.Time
		}
		if lastActivity.Valid {
			p.LastActivityAt = &lastActivity.Time
		}
		out = append(out, &p)
	}
	return out, nil
}

func (r *StatsRepository) Productivity(ctx context.Context, from, to time.Time) ([]*domain.ProductivityRow, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), u.email,
			COALESCE(pv.cnt, 0), COALESCE(sub.cnt, 0), COALESCE(ed.cnt, 0)
		FROM users u
		LEFT JOIN (
			SELECT verified_by uid, COUNT(*) cnt FROM page_verifications
			WHERE tenant_id = $1 AND is_verified AND verified_at >= $2 AND verified_at < $3
			GROUP BY verified_by
		) pv ON pv.uid = u.id
		LEFT JOIN (
			SELECT submitted_by uid, COUNT(*) cnt FROM documents
			WHERE tenant_id = $1 AND submitted_at >= $2 AND submitted_at < $3
			GROUP BY submitted_by
		) sub ON sub.uid = u.id
		LEFT JOIN (
			SELECT h.updated_by uid, COUNT(*) cnt FROM extracted_cells_history h
			JOIN documents d ON d.id = h.document_id
			WHERE d.tenant_id = $1 AND h.version > 1 AND h.created_at >= $2 AND h.created_at < $3
			GROUP BY h.updated_by
		) ed ON ed.uid = u.id
		WHERE u.tenant_id = $1
		ORDER BY COALESCE(pv.cnt, 0) DESC, COALESCE(sub.cnt, 0) DESC
	`, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query productivity: %w", err)
	}
	defer rows.Close()

	out := []*domain.ProductivityRow{}
	for rows.Next() {
		var p domain.ProductivityRow
		if err := rows.Scan(&p.UserID, &p.UserName, &p.Email, &p.PagesVerified, &p.FilesSubmitted, &p.CellsEdited); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, nil
}

func (r *StatsRepository) Timeseries(ctx context.Context, from, to time.Time) ([]*domain.TimeseriesPoint, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT to_char(g.day, 'YYYY-MM-DD'),
			COALESCE(pv.cnt, 0), COALESCE(sub.cnt, 0), COALESCE(ed.cnt, 0)
		FROM generate_series($2::date, $3::date, interval '1 day') AS g(day)
		LEFT JOIN (
			SELECT verified_at::date AS day, COUNT(*) cnt FROM page_verifications
			WHERE tenant_id = $1 AND is_verified GROUP BY 1
		) pv ON pv.day = g.day
		LEFT JOIN (
			SELECT submitted_at::date AS day, COUNT(*) cnt FROM documents
			WHERE tenant_id = $1 AND submitted_at IS NOT NULL GROUP BY 1
		) sub ON sub.day = g.day
		LEFT JOIN (
			SELECT h.created_at::date AS day, COUNT(*) cnt FROM extracted_cells_history h
			JOIN documents d ON d.id = h.document_id
			WHERE d.tenant_id = $1 AND h.version > 1 GROUP BY 1
		) ed ON ed.day = g.day
		ORDER BY g.day ASC
	`, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query timeseries: %w", err)
	}
	defer rows.Close()

	out := []*domain.TimeseriesPoint{}
	for rows.Next() {
		var p domain.TimeseriesPoint
		if err := rows.Scan(&p.Day, &p.PagesVerified, &p.FilesSubmitted, &p.CellsEdited); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, nil
}

func (r *StatsRepository) RecentActivity(ctx context.Context, limit int) ([]*domain.VerificationEvent, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.tenant_id, e.document_id, e.page_number, e.user_id, e.event_type, e.metadata, e.created_at,
			TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), COALESCE(d.name, '')
		FROM verification_events e
		LEFT JOIN users u ON u.id = e.user_id
		LEFT JOIN documents d ON d.id = e.document_id
		WHERE e.tenant_id = $1
		ORDER BY e.created_at DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent activity: %w", err)
	}
	defer rows.Close()

	out := []*domain.VerificationEvent{}
	for rows.Next() {
		var e domain.VerificationEvent
		var docID, userID uuid.NullUUID
		var page sql.NullInt32
		var metaRaw []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &docID, &page, &userID, &e.EventType, &metaRaw, &e.CreatedAt, &e.UserName, &e.DocumentName); err != nil {
			return nil, err
		}
		if docID.Valid {
			id := docID.UUID
			e.DocumentID = &id
		}
		if userID.Valid {
			id := userID.UUID
			e.UserID = &id
		}
		if page.Valid {
			v := int(page.Int32)
			e.PageNumber = &v
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &e.Metadata)
		}
		out = append(out, &e)
	}
	return out, nil
}
