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

type RoleRepository struct {
	db *DB
}

func NewRoleRepository(db *DB) *RoleRepository {
	return &RoleRepository{db: db}
}

const roleSelectCols = `
	r.id, r.tenant_id, r.name, r.description, r.is_system, r.created_at, r.updated_at,
	COALESCE(string_agg(DISTINCT p.code, ','), '') AS perms,
	(SELECT COUNT(*) FROM user_roles ur WHERE ur.role_id = r.id) AS user_count`

func scanRole(scan func(dest ...interface{}) error) (*domain.Role, error) {
	var role domain.Role
	var tenantID uuid.NullUUID
	var desc sql.NullString
	var permCSV string
	if err := scan(&role.ID, &tenantID, &role.Name, &desc, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt, &permCSV, &role.UserCount); err != nil {
		return nil, err
	}
	if tenantID.Valid {
		id := tenantID.UUID
		role.TenantID = &id
	}
	role.Description = desc.String
	if permCSV != "" {
		role.Permissions = strings.Split(permCSV, ",")
	} else {
		role.Permissions = []string{}
	}
	return &role, nil
}

// List returns the tenant's own roles plus shared system roles.
func (r *RoleRepository) List(ctx context.Context, tenantID uuid.UUID) ([]*domain.Role, error) {
	query := `
		SELECT ` + roleSelectCols + `
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		WHERE r.tenant_id = $1 OR r.tenant_id IS NULL
		GROUP BY r.id
		ORDER BY r.is_system DESC, r.name ASC
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}
	defer rows.Close()

	var roles []*domain.Role
	for rows.Next() {
		role, err := scanRole(rows.Scan)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func (r *RoleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	tenantID, _ := contextutil.GetTenantID(ctx)
	query := `
		SELECT ` + roleSelectCols + `
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		WHERE r.id = $1 AND (r.tenant_id = $2 OR r.tenant_id IS NULL)
		GROUP BY r.id
	`
	role, err := scanRole(r.db.QueryRowContext(ctx, query, id, tenantID).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get role: %w", err)
	}
	return role, nil
}

// Create inserts a new tenant-scoped (non-system) role.
func (r *RoleRepository) Create(ctx context.Context, role *domain.Role) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required to create role")
	}
	if role.ID == uuid.Nil {
		role.ID = uuid.New()
	}
	now := time.Now()
	role.CreatedAt = now
	role.UpdatedAt = now
	role.IsSystem = false
	role.TenantID = &tenantID

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO roles (id, tenant_id, name, description, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, FALSE, $5, $6)`,
		role.ID, tenantID, role.Name, role.Description, role.CreatedAt, role.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}
	return nil
}

// Update edits a tenant-owned role's name/description. System roles are immutable.
func (r *RoleRepository) Update(ctx context.Context, role *domain.Role) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required to update role")
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE roles SET name = $1, description = $2, updated_at = $3
		 WHERE id = $4 AND tenant_id = $5 AND is_system = FALSE`,
		role.Name, role.Description, time.Now(), role.ID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to update role: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("role not found, not owned by tenant, or is a system role")
	}
	return nil
}

func (r *RoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required to delete role")
	}
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM roles WHERE id = $1 AND tenant_id = $2 AND is_system = FALSE`, id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete role: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("role not found, not owned by tenant, or is a system role")
	}
	return nil
}

// SetPermissions replaces a tenant role's permission grants with exactly the
// given codes. System roles cannot be edited here.
func (r *RoleRepository) SetPermissions(ctx context.Context, roleID uuid.UUID, permissionCodes []string) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return errors.New("tenant context required")
	}

	// Guard: only tenant-owned, non-system roles may be modified.
	var ok bool
	if err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM roles WHERE id = $1 AND tenant_id = $2 AND is_system = FALSE)`,
		roleID, tenantID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errors.New("role not found, not owned by tenant, or is a system role")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM role_permissions WHERE role_id = $1", roleID); err != nil {
		return fmt.Errorf("failed to clear permissions: %w", err)
	}
	if len(permissionCodes) > 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO role_permissions (role_id, permission_id)
			 SELECT $1, p.id FROM permissions p WHERE p.code = ANY($2)
			 ON CONFLICT DO NOTHING`,
			roleID, permissionCodes); err != nil {
			return fmt.Errorf("failed to grant permissions: %w", err)
		}
	}
	return tx.Commit()
}

func (r *RoleRepository) ListPermissions(ctx context.Context) ([]*domain.Permission, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, code, description FROM permissions ORDER BY code ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", err)
	}
	defer rows.Close()

	var perms []*domain.Permission
	for rows.Next() {
		var p domain.Permission
		var desc sql.NullString
		if err := rows.Scan(&p.ID, &p.Code, &desc); err != nil {
			return nil, err
		}
		p.Description = desc.String
		// Category is the code prefix before the first dot, e.g. "documents.view" -> "documents".
		if idx := strings.Index(p.Code, "."); idx > 0 {
			p.Category = p.Code[:idx]
		} else {
			p.Category = "general"
		}
		perms = append(perms, &p)
	}
	return perms, nil
}
