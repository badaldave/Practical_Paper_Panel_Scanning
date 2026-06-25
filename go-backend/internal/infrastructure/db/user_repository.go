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

type UserRepository struct {
	db *DB
}

func NewUserRepository(db *DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && user.TenantID != tenantID {
		return errors.New("tenant mismatch in user creation")
	}

	query := `
		INSERT INTO users (id, tenant_id, email, password_hash, first_name, last_name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, query, user.ID, user.TenantID, user.Email, user.PasswordHash, user.FirstName, user.LastName, user.Status, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}

	if err == nil {
		query = `
			SELECT id, tenant_id, email, password_hash, first_name, last_name, status, created_at, updated_at
			FROM users
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, email, password_hash, first_name, last_name, status, created_at, updated_at
			FROM users
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var user domain.User
	err = r.db.QueryRowContext(ctx, query, args...).Scan(
		&user.ID,
		&user.TenantID,
		&user.Email,
		&user.PasswordHash,
		&user.FirstName,
		&user.LastName,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return &user, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*domain.User, error) {
	// Enforce context tenant validation if context has tenant_id
	ctxTenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tenantID != ctxTenantID {
		return nil, errors.New("tenant mismatch in get user by email")
	}

	query := `
		SELECT id, tenant_id, email, password_hash, first_name, last_name, status, created_at, updated_at
		FROM users
		WHERE tenant_id = $1 AND email = $2
	`
	var user domain.User
	err = r.db.QueryRowContext(ctx, query, tenantID, email).Scan(
		&user.ID,
		&user.TenantID,
		&user.Email,
		&user.PasswordHash,
		&user.FirstName,
		&user.LastName,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && user.TenantID != tenantID {
		return errors.New("tenant mismatch in user update")
	}

	query := `
		UPDATE users
		SET email = $1, first_name = $2, last_name = $3, status = $4, updated_at = $5
		WHERE id = $6 AND tenant_id = $7
	`
	user.UpdatedAt = time.Now()

	_, err = r.db.ExecContext(ctx, query, user.Email, user.FirstName, user.LastName, user.Status, user.UpdatedAt, user.ID, user.TenantID)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

func (r *UserRepository) AssignRole(ctx context.Context, userID uuid.UUID, roleName string) error {
	// Find role ID
	var roleID uuid.UUID
	err := r.db.QueryRowContext(ctx, "SELECT id FROM roles WHERE name = $1", roleName).Scan(&roleID)
	if err != nil {
		return fmt.Errorf("role not found: %s", roleName)
	}

	query := `
		INSERT INTO user_roles (user_id, role_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`
	_, err = r.db.ExecContext(ctx, query, userID, roleID)
	if err != nil {
		return fmt.Errorf("failed to assign role: %w", err)
	}
	return nil
}

func (r *UserRepository) GetUserRolesAndPermissions(ctx context.Context, userID uuid.UUID) ([]string, []string, error) {
	rolesQuery := `
		SELECT r.name 
		FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
	`
	rows, err := r.db.QueryContext(ctx, rolesQuery, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query user roles: %w", err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var roleName string
		if err := rows.Scan(&roleName); err != nil {
			return nil, nil, err
		}
		roles = append(roles, roleName)
	}

	permsQuery := `
		SELECT DISTINCT p.code
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1
	`
	pRows, err := r.db.QueryContext(ctx, permsQuery, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query user permissions: %w", err)
	}
	defer pRows.Close()

	var permissions []string
	for pRows.Next() {
		var permCode string
		if err := pRows.Scan(&permCode); err != nil {
			return nil, nil, err
		}
		permissions = append(permissions, permCode)
	}

	return roles, permissions, nil
}
