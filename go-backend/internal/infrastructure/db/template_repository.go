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

type TemplateRepository struct {
	db *DB
}

func NewTemplateRepository(db *DB) *TemplateRepository {
	return &TemplateRepository{db: db}
}

func (r *TemplateRepository) Create(ctx context.Context, tmpl *domain.Template) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tmpl.TenantID != tenantID {
		return errors.New("tenant mismatch in template creation")
	}

	query := `
		INSERT INTO templates (id, tenant_id, name, description, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if tmpl.ID == uuid.Nil {
		tmpl.ID = uuid.New()
	}
	now := time.Now()
	tmpl.CreatedAt = now
	tmpl.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, query, tmpl.ID, tmpl.TenantID, tmpl.Name, tmpl.Description, tmpl.IsActive, tmpl.CreatedAt, tmpl.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create template: %w", err)
	}
	return nil
}

func (r *TemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Template, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}

	if err == nil {
		query = `
			SELECT id, tenant_id, name, description, is_active, created_at, updated_at
			FROM templates
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, description, is_active, created_at, updated_at
			FROM templates
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var tmpl domain.Template
	err = r.db.QueryRowContext(ctx, query, args...).Scan(
		&tmpl.ID,
		&tmpl.TenantID,
		&tmpl.Name,
		&tmpl.Description,
		&tmpl.IsActive,
		&tmpl.CreatedAt,
		&tmpl.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get template by ID: %w", err)
	}

	return &tmpl, nil
}

func (r *TemplateRepository) GetByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.Template, error) {
	ctxTenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tenantID != ctxTenantID {
		return nil, errors.New("tenant mismatch in listing templates")
	}

	query := `
		SELECT id, tenant_id, name, description, is_active, created_at, updated_at
		FROM templates
		WHERE tenant_id = $1
		ORDER BY name ASC
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query templates: %w", err)
	}
	defer rows.Close()

	var templates []*domain.Template
	for rows.Next() {
		var tmpl domain.Template
		err := rows.Scan(
			&tmpl.ID,
			&tmpl.TenantID,
			&tmpl.Name,
			&tmpl.Description,
			&tmpl.IsActive,
			&tmpl.CreatedAt,
			&tmpl.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		templates = append(templates, &tmpl)
	}
	return templates, nil
}

func (r *TemplateRepository) Update(ctx context.Context, tmpl *domain.Template) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tmpl.TenantID != tenantID {
		return errors.New("tenant mismatch in template update")
	}

	query := `
		UPDATE templates
		SET name = $1, description = $2, is_active = $3, updated_at = $4
		WHERE id = $5 AND tenant_id = $6
	`
	tmpl.UpdatedAt = time.Now()

	_, err = r.db.ExecContext(ctx, query, tmpl.Name, tmpl.Description, tmpl.IsActive, tmpl.UpdatedAt, tmpl.ID, tmpl.TenantID)
	if err != nil {
		return fmt.Errorf("failed to update template: %w", err)
	}
	return nil
}

func (r *TemplateRepository) CreateVersion(ctx context.Context, v *domain.TemplateVersion) error {
	query := `
		INSERT INTO template_versions (id, template_id, version, is_active, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	v.CreatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, query, v.ID, v.TemplateID, v.Version, v.IsActive, v.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create template version: %w", err)
	}
	return nil
}

func (r *TemplateRepository) GetVersionByID(ctx context.Context, id uuid.UUID) (*domain.TemplateVersion, error) {
	query := `
		SELECT id, template_id, version, is_active, created_at
		FROM template_versions
		WHERE id = $1
	`
	var v domain.TemplateVersion
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&v.ID,
		&v.TemplateID,
		&v.Version,
		&v.IsActive,
		&v.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get template version: %w", err)
	}

	return &v, nil
}

func (r *TemplateRepository) GetLatestVersion(ctx context.Context, templateID uuid.UUID) (*domain.TemplateVersion, error) {
	query := `
		SELECT id, template_id, version, is_active, created_at
		FROM template_versions
		WHERE template_id = $1
		ORDER BY version DESC
		LIMIT 1
	`
	var v domain.TemplateVersion
	err := r.db.QueryRowContext(ctx, query, templateID).Scan(
		&v.ID,
		&v.TemplateID,
		&v.Version,
		&v.IsActive,
		&v.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest template version: %w", err)
	}

	return &v, nil
}

func (r *TemplateRepository) CreateField(ctx context.Context, field *domain.TemplateField) error {
	query := `
		INSERT INTO template_fields (id, template_version_id, name, field_type, bounding_box, expected_regex, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	bboxJSON, err := json.Marshal(field.BoundingBox)
	if err != nil {
		return err
	}

	if field.ID == uuid.Nil {
		field.ID = uuid.New()
	}
	now := time.Now()
	field.CreatedAt = now
	field.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, query, field.ID, field.TemplateVersionID, field.Name, field.FieldType, bboxJSON, field.ExpectedRegex, field.CreatedAt, field.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create template field: %w", err)
	}
	return nil
}

func (r *TemplateRepository) GetFields(ctx context.Context, versionID uuid.UUID) ([]*domain.TemplateField, error) {
	query := `
		SELECT id, template_version_id, name, field_type, bounding_box, expected_regex, created_at, updated_at
		FROM template_fields
		WHERE template_version_id = $1
		ORDER BY name ASC
	`
	rows, err := r.db.QueryContext(ctx, query, versionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query template fields: %w", err)
	}
	defer rows.Close()

	var fields []*domain.TemplateField
	for rows.Next() {
		var field domain.TemplateField
		var bboxBytes []byte
		err := rows.Scan(
			&field.ID,
			&field.TemplateVersionID,
			&field.Name,
			&field.FieldType,
			&bboxBytes,
			&field.ExpectedRegex,
			&field.CreatedAt,
			&field.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(bboxBytes, &field.BoundingBox); err != nil {
			return nil, err
		}
		fields = append(fields, &field)
	}
	return fields, nil
}
