package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

type AuditRepository struct {
	db *DB
}

func NewAuditRepository(db *DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) Log(ctx context.Context, log *domain.AuditLog) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && log.TenantID != tenantID {
		return errors.New("tenant mismatch in audit logging")
	}

	query := `
		INSERT INTO audit_logs (id, tenant_id, user_id, entity_type, entity_id, action, old_value, new_value, ip_address, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	oldJSON, err := json.Marshal(log.OldValue)
	if err != nil {
		return err
	}
	newJSON, err := json.Marshal(log.NewValue)
	if err != nil {
		return err
	}

	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}
	log.CreatedAt = time.Now()

	_, err = r.db.ExecContext(ctx, query, log.ID, log.TenantID, log.UserID, log.EntityType, log.EntityID, log.Action, oldJSON, newJSON, log.IPAddress, log.UserAgent, log.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to write audit log: %w", err)
	}
	return nil
}

func (r *AuditRepository) GetByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error) {
	ctxTenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tenantID != ctxTenantID {
		return nil, errors.New("tenant mismatch in listing audit logs")
	}

	query := `
		SELECT id, tenant_id, user_id, entity_type, entity_id, action, old_value, new_value, ip_address, user_agent, created_at
		FROM audit_logs
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*domain.AuditLog
	for rows.Next() {
		var log domain.AuditLog
		var oldBytes, newBytes []byte
		err := rows.Scan(
			&log.ID,
			&log.TenantID,
			&log.UserID,
			&log.EntityType,
			&log.EntityID,
			&log.Action,
			&oldBytes,
			&newBytes,
			&log.IPAddress,
			&log.UserAgent,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(oldBytes, &log.OldValue); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(newBytes, &log.NewValue); err != nil {
			return nil, err
		}
		logs = append(logs, &log)
	}
	return logs, nil
}

func (r *AuditRepository) GetByEntity(ctx context.Context, tenantID uuid.UUID, entityType string, entityID uuid.UUID) ([]*domain.AuditLog, error) {
	ctxTenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tenantID != ctxTenantID {
		return nil, errors.New("tenant mismatch in querying entity audit logs")
	}

	query := `
		SELECT id, tenant_id, user_id, entity_type, entity_id, action, old_value, new_value, ip_address, user_agent, created_at
		FROM audit_logs
		WHERE tenant_id = $1 AND entity_type = $2 AND entity_id = $3
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to query entity audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*domain.AuditLog
	for rows.Next() {
		var log domain.AuditLog
		var oldBytes, newBytes []byte
		err := rows.Scan(
			&log.ID,
			&log.TenantID,
			&log.UserID,
			&log.EntityType,
			&log.EntityID,
			&log.Action,
			&oldBytes,
			&newBytes,
			&log.IPAddress,
			&log.UserAgent,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(oldBytes, &log.OldValue); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(newBytes, &log.NewValue); err != nil {
			return nil, err
		}
		logs = append(logs, &log)
	}
	return logs, nil
}
