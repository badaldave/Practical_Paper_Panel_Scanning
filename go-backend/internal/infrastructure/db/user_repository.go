package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// List returns all users in the tenant with their role names populated.
func (r *UserRepository) List(ctx context.Context, tenantID uuid.UUID) ([]*domain.User, error) {
	ctxTenantID, err := contextutil.GetTenantID(ctx)
	if err == nil && tenantID != ctxTenantID {
		return nil, errors.New("tenant mismatch in listing users")
	}

	query := `
		SELECT u.id, u.tenant_id, u.email, u.first_name, u.last_name, u.status, u.created_at, u.updated_at,
			COALESCE(string_agg(DISTINCT r.name, ','), '') AS roles
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN roles r ON r.id = ur.role_id
		WHERE u.tenant_id = $1
		GROUP BY u.id
		ORDER BY u.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		var roleCSV string
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.FirstName, &u.LastName, &u.Status, &u.CreatedAt, &u.UpdatedAt, &roleCSV); err != nil {
			return nil, err
		}
		if roleCSV != "" {
			u.Roles = strings.Split(roleCSV, ",")
		} else {
			u.Roles = []string{}
		}
		users = append(users, &u)
	}
	return users, nil
}

// Delete removes a user (cascades to user_roles). Tenant-scoped.
func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}
	if err == nil {
		query = "DELETE FROM users WHERE id = $1 AND tenant_id = $2"
		args = []interface{}{id, tenantID}
	} else {
		query = "DELETE FROM users WHERE id = $1"
		args = []interface{}{id}
	}
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// UpdatePassword sets a new bcrypt hash for the user. Tenant-scoped.
func (r *UserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	var query string
	var args []interface{}
	if err == nil {
		query = "UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4"
		args = []interface{}{passwordHash, time.Now(), id, tenantID}
	} else {
		query = "UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3"
		args = []interface{}{passwordHash, time.Now(), id}
	}
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	return nil
}

// SetRoles replaces the user's role assignments with exactly roleIDs.
func (r *UserRepository) SetRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM user_roles WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("failed to clear roles: %w", err)
	}
	for _, rid := range roleIDs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", userID, rid); err != nil {
			return fmt.Errorf("failed to assign role: %w", err)
		}
	}
	return tx.Commit()
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
