package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	Status       string    `json:"status"` // 'active', 'suspended', 'pending'
	Roles        []string  `json:"roles,omitempty"`
	Permissions  []string  `json:"permissions,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Role struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenant_id,omitempty"` // nil => shared system role
	Name        string     `json:"name"`
	Description string     `json:"description"`
	IsSystem    bool       `json:"is_system"`
	Permissions []string   `json:"permissions,omitempty"` // permission codes
	UserCount   int        `json:"user_count"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Permission struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	AssignRole(ctx context.Context, userID uuid.UUID, roleName string) error
	GetUserRolesAndPermissions(ctx context.Context, userID uuid.UUID) ([]string, []string, error)
	// Management surface
	List(ctx context.Context, tenantID uuid.UUID) ([]*User, error)
	Delete(ctx context.Context, id uuid.UUID) error
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	SetRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error
}

// RoleRepository manages tenant-scoped and shared system roles plus the
// permission catalog and role->permission grants.
type RoleRepository interface {
	List(ctx context.Context, tenantID uuid.UUID) ([]*Role, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Role, error)
	Create(ctx context.Context, role *Role) error
	Update(ctx context.Context, role *Role) error
	Delete(ctx context.Context, id uuid.UUID) error
	SetPermissions(ctx context.Context, roleID uuid.UUID, permissionCodes []string) error
	ListPermissions(ctx context.Context) ([]*Permission, error)
}
