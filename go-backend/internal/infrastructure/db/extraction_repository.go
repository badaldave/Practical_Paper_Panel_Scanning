package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

type ExtractionRepository struct {
	db *DB
}

func NewExtractionRepository(db *DB) *ExtractionRepository {
	return &ExtractionRepository{db: db}
}

func (r *ExtractionRepository) CreateExtraction(ctx context.Context, ext *domain.Extraction) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && ext.TenantID != tenantID {
		return errors.New("tenant mismatch in extraction creation")
	}

	query := `
		INSERT INTO extractions (id, tenant_id, document_id, template_version_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if ext.ID == uuid.Nil {
		ext.ID = uuid.New()
	}
	now := time.Now()
	ext.CreatedAt = now
	ext.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, query, ext.ID, ext.TenantID, ext.DocumentID, ext.TemplateVersionID, ext.Status, ext.CreatedAt, ext.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create extraction: %w", err)
	}
	return nil
}

func (r *ExtractionRepository) GetExtractionByDocID(ctx context.Context, docID uuid.UUID) (*domain.Extraction, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}

	if err == nil {
		query = `
			SELECT id, tenant_id, document_id, template_version_id, status, created_at, updated_at
			FROM extractions
			WHERE document_id = $1 AND tenant_id = $2
		`
		args = []interface{}{docID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, document_id, template_version_id, status, created_at, updated_at
			FROM extractions
			WHERE document_id = $1
		`
		args = []interface{}{docID}
	}

	var ext domain.Extraction
	err = r.db.QueryRowContext(ctx, query, args...).Scan(
		&ext.ID,
		&ext.TenantID,
		&ext.DocumentID,
		&ext.TemplateVersionID,
		&ext.Status,
		&ext.CreatedAt,
		&ext.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get extraction by doc ID: %w", err)
	}

	return &ext, nil
}

func (r *ExtractionRepository) CreateTable(ctx context.Context, table *domain.ExtractedTable) error {
	query := `
		INSERT INTO extracted_tables (id, extraction_id, page_number, table_index, bounding_box, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	bboxJSON, err := json.Marshal(table.BoundingBox)
	if err != nil {
		return err
	}

	if table.ID == uuid.Nil {
		table.ID = uuid.New()
	}
	now := time.Now()
	table.CreatedAt = now
	table.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, query, table.ID, table.ExtractionID, table.PageNumber, table.TableIndex, bboxJSON, table.CreatedAt, table.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create extracted table: %w", err)
	}
	return nil
}

func (r *ExtractionRepository) GetTablesByExtraction(ctx context.Context, extID uuid.UUID) ([]*domain.ExtractedTable, error) {
	query := `
		SELECT id, extraction_id, page_number, table_index, bounding_box, created_at, updated_at
		FROM extracted_tables
		WHERE extraction_id = $1
		ORDER BY page_number ASC, table_index ASC
	`
	rows, err := r.db.QueryContext(ctx, query, extID)
	if err != nil {
		return nil, fmt.Errorf("failed to query extracted tables: %w", err)
	}
	defer rows.Close()

	var tables []*domain.ExtractedTable
	for rows.Next() {
		var table domain.ExtractedTable
		var bboxBytes []byte
		err := rows.Scan(
			&table.ID,
			&table.ExtractionID,
			&table.PageNumber,
			&table.TableIndex,
			&bboxBytes,
			&table.CreatedAt,
			&table.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(bboxBytes, &table.BoundingBox); err != nil {
			return nil, err
		}
		tables = append(tables, &table)
	}
	return tables, nil
}

func (r *ExtractionRepository) CreateRow(ctx context.Context, row *domain.ExtractedRow) error {
	query := `
		INSERT INTO extracted_rows (id, table_id, row_index, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	now := time.Now()
	row.CreatedAt = now
	row.UpdatedAt = now

	_, err := r.db.ExecContext(ctx, query, row.ID, row.TableID, row.RowIndex, row.CreatedAt, row.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create extracted row: %w", err)
	}
	return nil
}

func (r *ExtractionRepository) GetRowsByTable(ctx context.Context, tableID uuid.UUID) ([]*domain.ExtractedRow, error) {
	query := `
		SELECT id, table_id, row_index, created_at, updated_at
		FROM extracted_rows
		WHERE table_id = $1
		ORDER BY row_index ASC
	`
	rows, err := r.db.QueryContext(ctx, query, tableID)
	if err != nil {
		return nil, fmt.Errorf("failed to query extracted rows: %w", err)
	}
	defer rows.Close()

	var rowsList []*domain.ExtractedRow
	for rows.Next() {
		var row domain.ExtractedRow
		err := rows.Scan(
			&row.ID,
			&row.TableID,
			&row.RowIndex,
			&row.CreatedAt,
			&row.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		rowsList = append(rowsList, &row)
	}
	return rowsList, nil
}

func (r *ExtractionRepository) SaveCell(ctx context.Context, cell *domain.ExtractedCell) error {
	// First, let's verify tenant context owns this document
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil {
		var exists bool
		err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2)", cell.DocumentID, tenantID).Scan(&exists)
		if err != nil || !exists {
			return errors.New("document not found or tenant mismatch")
		}
	}

	// Determine the version
	var currentMaxVersion int
	vQuery := `
		SELECT COALESCE(MAX(version), 0)
		FROM extracted_cells
		WHERE document_id = $1 AND page_number = $2 AND row_index = $3 AND column_index = $4
	`
	err = r.db.QueryRowContext(ctx, vQuery, cell.DocumentID, cell.PageNumber, cell.RowIndex, cell.ColumnIndex).Scan(&currentMaxVersion)
	if err != nil {
		return fmt.Errorf("failed to get max cell version: %w", err)
	}

	cell.Version = currentMaxVersion + 1
	if cell.ID == uuid.Nil {
		cell.ID = uuid.New()
	}
	now := time.Now()
	cell.CreatedAt = now
	cell.UpdatedAt = now

	bboxJSON, err := json.Marshal(cell.BBox)
	if err != nil {
		return err
	}

	// Insert new cell version
	insertQuery := `
		INSERT INTO extracted_cells (id, document_id, page_number, row_index, column_index, original_value, current_value, confidence, bbox, version, created_by, updated_by, created_at, updated_at, is_inferred)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err = r.db.ExecContext(ctx, insertQuery,
		cell.ID, cell.DocumentID, cell.PageNumber, cell.RowIndex, cell.ColumnIndex,
		cell.OriginalValue, cell.CurrentValue, cell.Confidence, bboxJSON,
		cell.Version, cell.CreatedBy, cell.UpdatedBy, cell.CreatedAt, cell.UpdatedAt, cell.IsInferred,
	)
	if err != nil {
		return fmt.Errorf("failed to insert cell version: %w", err)
	}

	// Record in history table
	historyQuery := `
		INSERT INTO extracted_cells_history (id, cell_id, document_id, page_number, row_index, column_index, value, confidence, bbox, version, updated_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	historyID := uuid.New()
	_, err = r.db.ExecContext(ctx, historyQuery,
		historyID, cell.ID, cell.DocumentID, cell.PageNumber, cell.RowIndex, cell.ColumnIndex,
		cell.CurrentValue, cell.Confidence, bboxJSON, cell.Version, cell.UpdatedBy, cell.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert cell history: %w", err)
	}

	return nil
}

func (r *ExtractionRepository) GetActiveCellsByDocID(ctx context.Context, docID uuid.UUID) ([]*domain.ExtractedCell, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil {
		var exists bool
		err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2)", docID, tenantID).Scan(&exists)
		if err != nil || !exists {
			return nil, errors.New("document not found or tenant mismatch")
		}
	}

	query := `
		SELECT id, document_id, page_number, row_index, column_index, original_value, current_value, confidence, bbox, is_inferred, version, created_by, updated_by, created_at, updated_at
		FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY page_number, row_index, column_index ORDER BY version DESC) as rn
			FROM extracted_cells
			WHERE document_id = $1
		) t
		WHERE t.rn = 1
		ORDER BY page_number ASC, row_index ASC, column_index ASC
	`
	rows, err := r.db.QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("failed to query active cells: %w", err)
	}
	defer rows.Close()

	var cells []*domain.ExtractedCell
	for rows.Next() {
		var cell domain.ExtractedCell
		var bboxBytes []byte
		err := rows.Scan(
			&cell.ID,
			&cell.DocumentID,
			&cell.PageNumber,
			&cell.RowIndex,
			&cell.ColumnIndex,
			&cell.OriginalValue,
			&cell.CurrentValue,
			&cell.Confidence,
			&bboxBytes,
			&cell.IsInferred,
			&cell.Version,
			&cell.CreatedBy,
			&cell.UpdatedBy,
			&cell.CreatedAt,
			&cell.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(bboxBytes, &cell.BBox); err != nil {
			return nil, err
		}
		cells = append(cells, &cell)
	}
	return cells, nil
}

func (r *ExtractionRepository) GetCellHistory(ctx context.Context, docID uuid.UUID, pageNum, rowIdx, colIdx int) ([]*domain.ExtractedCell, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil {
		var exists bool
		err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2)", docID, tenantID).Scan(&exists)
		if err != nil || !exists {
			return nil, errors.New("document not found or tenant mismatch")
		}
	}

	query := `
		SELECT id, document_id, page_number, row_index, column_index, original_value, current_value, confidence, bbox, is_inferred, version, created_by, updated_by, created_at, updated_at
		FROM extracted_cells
		WHERE document_id = $1 AND page_number = $2 AND row_index = $3 AND column_index = $4
		ORDER BY version DESC
	`
	rows, err := r.db.QueryContext(ctx, query, docID, pageNum, rowIdx, colIdx)
	if err != nil {
		return nil, fmt.Errorf("failed to query cell history: %w", err)
	}
	defer rows.Close()

	var cells []*domain.ExtractedCell
	for rows.Next() {
		var cell domain.ExtractedCell
		var bboxBytes []byte
		err := rows.Scan(
			&cell.ID,
			&cell.DocumentID,
			&cell.PageNumber,
			&cell.RowIndex,
			&cell.ColumnIndex,
			&cell.OriginalValue,
			&cell.CurrentValue,
			&cell.Confidence,
			&bboxBytes,
			&cell.IsInferred,
			&cell.Version,
			&cell.CreatedBy,
			&cell.UpdatedBy,
			&cell.CreatedAt,
			&cell.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(bboxBytes, &cell.BBox); err != nil {
			return nil, err
		}
		cells = append(cells, &cell)
	}
	return cells, nil
}

func (r *ExtractionRepository) CreateFeedback(ctx context.Context, f *domain.CorrectionFeedback) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && f.TenantID != tenantID {
		return errors.New("tenant mismatch in feedback creation")
	}

	query := `
		INSERT INTO correction_feedback (id, tenant_id, document_type, original_value, corrected_value, context_left, context_right, image_region_path, is_applied_in_training, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	f.CreatedAt = time.Now()

	_, err = r.db.ExecContext(ctx, query, f.ID, f.TenantID, f.DocumentType, f.OriginalValue, f.CorrectedValue, f.ContextLeft, f.ContextRight, f.ImageRegionPath, f.IsAppliedInTraining, f.CreatedBy, f.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create feedback: %w", err)
	}
	return nil
}

func (r *ExtractionRepository) GetPendingFeedback(ctx context.Context, limit int) ([]*domain.CorrectionFeedback, error) {
	query := `
		SELECT id, tenant_id, document_type, original_value, corrected_value, context_left, context_right, image_region_path, is_applied_in_training, created_by, created_at
		FROM correction_feedback
		WHERE is_applied_in_training = FALSE
		ORDER BY created_at ASC
		LIMIT $1
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending feedback: %w", err)
	}
	defer rows.Close()

	var feedbackList []*domain.CorrectionFeedback
	for rows.Next() {
		var f domain.CorrectionFeedback
		err := rows.Scan(
			&f.ID,
			&f.TenantID,
			&f.DocumentType,
			&f.OriginalValue,
			&f.CorrectedValue,
			&f.ContextLeft,
			&f.ContextRight,
			&f.ImageRegionPath,
			&f.IsAppliedInTraining,
			&f.CreatedBy,
			&f.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		feedbackList = append(feedbackList, &f)
	}
	return feedbackList, nil
}

// LookupExaminerByMobile resolves a mobile number to the best-known examiner name
// for the tenant. Priority (point #3 logic): the most recent HUMAN-VERIFIED
// correction (a non-inferred name on a submitted document, matched by mobile) wins
// over the seeded examiner_registry, irrespective of vote counts. Ambiguous
// registry numbers (reused across people) never auto-fill.
func (r *ExtractionRepository) LookupExaminerByMobile(ctx context.Context, mobile string) (*domain.ExaminerMatch, error) {
	digits := onlyDigitsStr(mobile)
	if len(digits) < 10 {
		return &domain.ExaminerMatch{Mobile: digits}, nil
	}
	key := digits[len(digits)-10:]

	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, errors.New("tenant context required")
	}

	// 1. Latest human-verified correction (newest wins).
	verifiedQuery := `
		WITH latest AS (
			SELECT DISTINCT ON (c.document_id, c.page_number, c.row_index, c.column_index)
				c.document_id, c.page_number, c.row_index, c.column_index,
				c.current_value, c.is_inferred, c.updated_at
			FROM extracted_cells c
			JOIN documents d ON d.id = c.document_id
			WHERE d.tenant_id = $1
			  AND d.verification_status = 'submitted'
			  AND c.column_index IN (2, 3)
			ORDER BY c.document_id, c.page_number, c.row_index, c.column_index, c.version DESC
		)
		SELECT n.current_value
		FROM latest n
		JOIN latest m
		  ON n.document_id = m.document_id
		 AND n.page_number = m.page_number
		 AND n.row_index = m.row_index
		WHERE n.column_index = 2 AND m.column_index = 3
		  AND n.is_inferred = FALSE
		  AND length(regexp_replace(n.current_value, '[^A-Za-z]', '', 'g')) >= 2
		  AND right(regexp_replace(m.current_value, '\D', '', 'g'), 10) = $2
		ORDER BY GREATEST(n.updated_at, m.updated_at) DESC
		LIMIT 1
	`
	var name string
	err = r.db.QueryRowContext(ctx, verifiedQuery, tenantID, key).Scan(&name)
	if err == nil && strings.TrimSpace(name) != "" {
		return &domain.ExaminerMatch{Mobile: key, Name: name, Source: "verified"}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("verified lookup failed: %w", err)
	}

	// 2. Seeded registry fallback. Non-ambiguous match preferred; if only ambiguous
	// numbers exist we report ambiguous and return no name (never guess).
	registryQuery := `
		SELECT canonical_name, is_ambiguous
		FROM examiner_registry
		WHERE tenant_id = $1
		  AND canonical_name IS NOT NULL
		  AND right(regexp_replace(mobile, '\D', '', 'g'), 10) = $2
		ORDER BY is_ambiguous ASC, times_seen DESC
		LIMIT 1
	`
	var regName string
	var ambiguous bool
	err = r.db.QueryRowContext(ctx, registryQuery, tenantID, key).Scan(&regName, &ambiguous)
	if err != nil {
		// Registry table may be absent (migration not applied) or simply no row —
		// treat as "unknown" rather than a hard error.
		return &domain.ExaminerMatch{Mobile: key}, nil
	}
	if ambiguous {
		return &domain.ExaminerMatch{Mobile: key, Ambiguous: true}, nil
	}
	return &domain.ExaminerMatch{Mobile: key, Name: regName, Source: "registry"}, nil
}

// onlyDigitsStr strips everything but ASCII digits.
func onlyDigitsStr(s string) string {
	var b strings.Builder
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func (r *ExtractionRepository) MarkFeedbackApplied(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	query := `
		UPDATE correction_feedback
		SET is_applied_in_training = TRUE
		WHERE id = ANY($1)
	`
	_, err := r.db.ExecContext(ctx, query, ids)
	if err != nil {
		return fmt.Errorf("failed to mark feedback applied: %w", err)
	}
	return nil
}
