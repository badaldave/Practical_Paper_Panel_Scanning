package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type BoundingBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Template struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TemplateVersion struct {
	ID         uuid.UUID `json:"id"`
	TemplateID uuid.UUID `json:"template_id"`
	Version    int       `json:"version"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
}

type TemplateField struct {
	ID                uuid.UUID   `json:"id"`
	TemplateVersionID uuid.UUID   `json:"template_version_id"`
	Name              string      `json:"name"`
	FieldType         string      `json:"field_type"` // 'text', 'numeric', 'date', 'table_region'
	BoundingBox       BoundingBox `json:"bounding_box"`
	ExpectedRegex     string      `json:"expected_regex,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

type TemplateRepository interface {
	Create(ctx context.Context, template *Template) error
	GetByID(ctx context.Context, id uuid.UUID) (*Template, error)
	GetByTenant(ctx context.Context, tenantID uuid.UUID) ([]*Template, error)
	Update(ctx context.Context, template *Template) error
	
	CreateVersion(ctx context.Context, version *TemplateVersion) error
	GetVersionByID(ctx context.Context, id uuid.UUID) (*TemplateVersion, error)
	GetLatestVersion(ctx context.Context, templateID uuid.UUID) (*TemplateVersion, error)
	
	CreateField(ctx context.Context, field *TemplateField) error
	GetFields(ctx context.Context, versionID uuid.UUID) ([]*TemplateField, error)
}
