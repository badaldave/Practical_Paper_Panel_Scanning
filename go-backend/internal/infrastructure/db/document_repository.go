package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

type DocumentRepository struct {
	db *DB
}

func NewDocumentRepository(db *DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

func (r *DocumentRepository) Create(ctx context.Context, doc *domain.Document) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && doc.TenantID != tenantID {
		return errors.New("tenant mismatch in document creation")
	}

	query := `
		INSERT INTO documents (id, tenant_id, name, file_path, file_size, mime_type, status, progress_percentage, template_id, uploaded_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	if doc.ID == uuid.Nil {
		doc.ID = uuid.New()
	}
	now := time.Now()
	doc.CreatedAt = now
	doc.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, query, doc.ID, doc.TenantID, doc.Name, doc.FilePath, doc.FileSize, doc.MimeType, doc.Status, doc.ProgressPercentage, doc.TemplateID, doc.UploadedBy, doc.CreatedAt, doc.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create document: %w", err)
	}
	return nil
}

func (r *DocumentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Document, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}

	if err == nil {
		query = `
			SELECT id, tenant_id, name, file_path, file_size, mime_type, status, progress_percentage, template_id, uploaded_by, created_at, updated_at
			FROM documents
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, file_path, file_size, mime_type, status, progress_percentage, template_id, uploaded_by, created_at, updated_at
			FROM documents
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var doc domain.Document
	err = r.db.QueryRowContext(ctx, query, args...).Scan(
		&doc.ID,
		&doc.TenantID,
		&doc.Name,
		&doc.FilePath,
		&doc.FileSize,
		&doc.MimeType,
		&doc.Status,
		&doc.ProgressPercentage,
		&doc.TemplateID,
		&doc.UploadedBy,
		&doc.CreatedAt,
		&doc.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get document by ID: %w", err)
	}

	return &doc, nil
}

func (r *DocumentRepository) GetByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.Document, error) {
	ctxTenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tenantID != ctxTenantID {
		return nil, errors.New("tenant mismatch in listing documents")
	}

	query := `
		SELECT id, tenant_id, name, file_path, file_size, mime_type, status, progress_percentage, template_id, uploaded_by, created_at, updated_at
		FROM documents
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}
	defer rows.Close()

	var docs []*domain.Document
	for rows.Next() {
		var doc domain.Document
		err := rows.Scan(
			&doc.ID,
			&doc.TenantID,
			&doc.Name,
			&doc.FilePath,
			&doc.FileSize,
			&doc.MimeType,
			&doc.Status,
			&doc.ProgressPercentage,
			&doc.TemplateID,
			&doc.UploadedBy,
			&doc.CreatedAt,
			&doc.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		docs = append(docs, &doc)
	}
	return docs, nil
}

func (r *DocumentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}

	if err == nil {
		query = `
			UPDATE documents
			SET status = $1, updated_at = $2
			WHERE id = $3 AND tenant_id = $4
		`
		args = []interface{}{status, time.Now(), id, tenantID}
	} else {
		query = `
			UPDATE documents
			SET status = $1, updated_at = $2
			WHERE id = $3
		`
		args = []interface{}{status, time.Now(), id}
	}

	_, err = r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update document status: %w", err)
	}
	return nil
}

func (r *DocumentRepository) UpdateTemplate(ctx context.Context, id uuid.UUID, templateID uuid.UUID) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}

	if err == nil {
		query = `
			UPDATE documents
			SET template_id = $1, updated_at = $2
			WHERE id = $3 AND tenant_id = $4
		`
		args = []interface{}{templateID, time.Now(), id, tenantID}
	} else {
		query = `
			UPDATE documents
			SET template_id = $1, updated_at = $2
			WHERE id = $3
		`
		args = []interface{}{templateID, time.Now(), id}
	}

	_, err = r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update document template: %w", err)
	}
	return nil
}

func (r *DocumentRepository) CreatePage(ctx context.Context, page *domain.DocumentPage) error {
	query := `
		INSERT INTO document_pages (id, document_id, page_number, image_path, width, height, status, college_code, college_name, subject_code, subject_name, faculty, total_candidates, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	if page.ID == uuid.Nil {
		page.ID = uuid.New()
	}
	now := time.Now()
	page.CreatedAt = now
	page.UpdatedAt = now

	_, err := r.db.ExecContext(ctx, query, page.ID, page.DocumentID, page.PageNumber, page.ImagePath, page.Width, page.Height, page.Status, page.CollegeCode, page.CollegeName, page.SubjectCode, page.SubjectName, page.Faculty, page.TotalCandidates, page.CreatedAt, page.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create document page: %w", err)
	}
	return nil
}

func (r *DocumentRepository) GetPages(ctx context.Context, docID uuid.UUID) ([]*domain.DocumentPage, error) {
	// First confirm document belongs to tenant (if tenant is in context)
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil {
		var exists bool
		err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2)", docID, tenantID).Scan(&exists)
		if err != nil || !exists {
			return nil, errors.New("document not found or tenant mismatch")
		}
	}

	query := `
		SELECT id, document_id, page_number, image_path, width, height, status, college_code, college_name, subject_code, subject_name, faculty, total_candidates, created_at, updated_at
		FROM document_pages
		WHERE document_id = $1
		ORDER BY page_number ASC
	`
	rows, err := r.db.QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("failed to query document pages: %w", err)
	}
	defer rows.Close()

	var pages []*domain.DocumentPage
	for rows.Next() {
		var page domain.DocumentPage
		err := rows.Scan(
			&page.ID,
			&page.DocumentID,
			&page.PageNumber,
			&page.ImagePath,
			&page.Width,
			&page.Height,
			&page.Status,
			&page.CollegeCode,
			&page.CollegeName,
			&page.SubjectCode,
			&page.SubjectName,
			&page.Faculty,
			&page.TotalCandidates,
			&page.CreatedAt,
			&page.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		pages = append(pages, &page)
	}
	return pages, nil
}

func (r *DocumentRepository) GetPageByNumber(ctx context.Context, docID uuid.UUID, pageNum int) (*domain.DocumentPage, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil {
		var exists bool
		err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2)", docID, tenantID).Scan(&exists)
		if err != nil || !exists {
			return nil, errors.New("document not found or tenant mismatch")
		}
	}

	query := `
		SELECT id, document_id, page_number, image_path, width, height, status, college_code, college_name, subject_code, subject_name, faculty, total_candidates, created_at, updated_at
		FROM document_pages
		WHERE document_id = $1 AND page_number = $2
	`
	var page domain.DocumentPage
	err = r.db.QueryRowContext(ctx, query, docID, pageNum).Scan(
		&page.ID,
		&page.DocumentID,
		&page.PageNumber,
		&page.ImagePath,
		&page.Width,
		&page.Height,
		&page.Status,
		&page.CollegeCode,
		&page.CollegeName,
		&page.SubjectCode,
		&page.SubjectName,
		&page.Faculty,
		&page.TotalCandidates,
		&page.CreatedAt,
		&page.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get page by page number: %w", err)
	}

	return &page, nil
}

func (r *DocumentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}

	if err == nil {
		query = "DELETE FROM documents WHERE id = $1 AND tenant_id = $2"
		args = []interface{}{id, tenantID}
	} else {
		query = "DELETE FROM documents WHERE id = $1"
		args = []interface{}{id}
	}

	_, err = r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}
	return nil
}

func (r *DocumentRepository) DeleteAll(ctx context.Context, tenantID uuid.UUID) error {
	ctxTenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tenantID != ctxTenantID {
		return errors.New("tenant mismatch in deleting documents")
	}

	query := "DELETE FROM documents WHERE tenant_id = $1"
	_, err = r.db.ExecContext(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete all documents: %w", err)
	}
	return nil
}

func (r *DocumentRepository) UpdatePageMetadata(ctx context.Context, docID uuid.UUID, pageNum int, page *domain.DocumentPage) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil {
		var exists bool
		err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1 AND tenant_id = $2)", docID, tenantID).Scan(&exists)
		if err != nil || !exists {
			return errors.New("document not found or tenant mismatch")
		}
	}

	query := `
		UPDATE document_pages
		SET college_code = $1, college_name = $2, subject_code = $3, subject_name = $4, faculty = $5, total_candidates = $6, updated_at = $7
		WHERE document_id = $8 AND page_number = $9
	`
	_, err = r.db.ExecContext(ctx, query,
		page.CollegeCode,
		page.CollegeName,
		page.SubjectCode,
		page.SubjectName,
		page.Faculty,
		page.TotalCandidates,
		time.Now(),
		docID,
		pageNum,
	)
	if err != nil {
		return fmt.Errorf("failed to update page metadata: %w", err)
	}
	return nil
}

