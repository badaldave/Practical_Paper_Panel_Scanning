package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AuditLog struct {
	ID         uuid.UUID              `json:"id"`
	TenantID   uuid.UUID              `json:"tenant_id"`
	UserID     *uuid.UUID             `json:"user_id,omitempty"`
	EntityType string                 `json:"entity_type"` // 'document', 'cell', 'template', 'user'
	EntityID   uuid.UUID              `json:"entity_id"`
	Action     string                 `json:"action"`      // 'uploaded', 'updated', 'deleted', 'exported'
	OldValue   map[string]interface{} `json:"old_value,omitempty"`
	NewValue   map[string]interface{} `json:"new_value,omitempty"`
	IPAddress  string                 `json:"ip_address"`
	UserAgent  string                 `json:"user_agent"`
	CreatedAt  time.Time              `json:"created_at"`
}

type AuditRepository interface {
	Log(ctx context.Context, log *AuditLog) error
	GetByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*AuditLog, error)
	GetByEntity(ctx context.Context, tenantID uuid.UUID, entityType string, entityID uuid.UUID) ([]*AuditLog, error)
}
