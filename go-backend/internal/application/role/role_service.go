package role

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

type SaveRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type RoleService struct {
	roleRepo domain.RoleRepository
}

func NewRoleService(rr domain.RoleRepository) *RoleService {
	return &RoleService{roleRepo: rr}
}

func (s *RoleService) List(ctx context.Context) ([]*domain.Role, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}
	return s.roleRepo.List(ctx, tenantID)
}

func (s *RoleService) Get(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	return s.roleRepo.GetByID(ctx, id)
}

func (s *RoleService) ListPermissions(ctx context.Context) ([]*domain.Permission, error) {
	return s.roleRepo.ListPermissions(ctx)
}

func (s *RoleService) Create(ctx context.Context, req SaveRoleRequest) (*domain.Role, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, errors.New("role name is required")
	}
	role := &domain.Role{Name: req.Name, Description: req.Description}
	if err := s.roleRepo.Create(ctx, role); err != nil {
		return nil, err
	}
	if len(req.Permissions) > 0 {
		if err := s.roleRepo.SetPermissions(ctx, role.ID, req.Permissions); err != nil {
			return nil, err
		}
	}
	return s.roleRepo.GetByID(ctx, role.ID)
}

func (s *RoleService) Update(ctx context.Context, id uuid.UUID, req SaveRoleRequest) (*domain.Role, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, errors.New("role name is required")
	}
	role := &domain.Role{ID: id, Name: req.Name, Description: req.Description}
	if err := s.roleRepo.Update(ctx, role); err != nil {
		return nil, err
	}
	if req.Permissions != nil {
		if err := s.roleRepo.SetPermissions(ctx, id, req.Permissions); err != nil {
			return nil, err
		}
	}
	return s.roleRepo.GetByID(ctx, id)
}

func (s *RoleService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.roleRepo.Delete(ctx, id)
}
