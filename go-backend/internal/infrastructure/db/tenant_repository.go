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
)

type TenantRepository struct {
	db *DB
}

func NewTenantRepository(db *DB) *TenantRepository {
	return &TenantRepository{db: db}
}

func (r *TenantRepository) Create(ctx context.Context, tenant *domain.Tenant) error {
	query := `
		INSERT INTO tenants (id, name, domain, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	settingsJSON, err := json.Marshal(tenant.Settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if tenant.ID == uuid.Nil {
		tenant.ID = uuid.New()
	}
	now := time.Now()
	tenant.CreatedAt = now
	tenant.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, query, tenant.ID, tenant.Name, tenant.Domain, settingsJSON, tenant.CreatedAt, tenant.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create tenant: %w", err)
	}
	return nil
}

func (r *TenantRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Tenant, error) {
	query := `
		SELECT id, name, domain, settings, created_at, updated_at
		FROM tenants
		WHERE id = $1
	`
	var tenant domain.Tenant
	var settingsBytes []byte

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Domain,
		&settingsBytes,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get tenant by ID: %w", err)
	}

	if err := json.Unmarshal(settingsBytes, &tenant.Settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	return &tenant, nil
}

func (r *TenantRepository) GetByDomain(ctx context.Context, domainName string) (*domain.Tenant, error) {
	query := `
		SELECT id, name, domain, settings, created_at, updated_at
		FROM tenants
		WHERE domain = $1
	`
	var tenant domain.Tenant
	var settingsBytes []byte

	err := r.db.QueryRowContext(ctx, query, domainName).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Domain,
		&settingsBytes,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get tenant by domain: %w", err)
	}

	if err := json.Unmarshal(settingsBytes, &tenant.Settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	return &tenant, nil
}

func (r *TenantRepository) Update(ctx context.Context, tenant *domain.Tenant) error {
	query := `
		UPDATE tenants
		SET name = $1, domain = $2, settings = $3, updated_at = $4
		WHERE id = $5
	`
	settingsJSON, err := json.Marshal(tenant.Settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	tenant.UpdatedAt = time.Now()

	_, err = r.db.ExecContext(ctx, query, tenant.Name, tenant.Domain, settingsJSON, tenant.UpdatedAt, tenant.ID)
	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}
	return nil
}
