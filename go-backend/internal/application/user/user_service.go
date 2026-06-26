package user

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
	"university-result-processing/backend/internal/pkg/crypto"
)

type CreateUserRequest struct {
	Email     string      `json:"email"`
	Password  string      `json:"password"`
	FirstName string      `json:"first_name"`
	LastName  string      `json:"last_name"`
	Status    string      `json:"status"`
	RoleIDs   []uuid.UUID `json:"role_ids"`
}

type UpdateUserRequest struct {
	Email     *string      `json:"email"`
	FirstName *string      `json:"first_name"`
	LastName  *string      `json:"last_name"`
	Status    *string      `json:"status"`
	RoleIDs   *[]uuid.UUID `json:"role_ids"`
}

type UserService struct {
	userRepo domain.UserRepository
}

func NewUserService(ur domain.UserRepository) *UserService {
	return &UserService{userRepo: ur}
}

func (s *UserService) List(ctx context.Context) ([]*domain.User, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}
	return s.userRepo.List(ctx, tenantID)
}

func (s *UserService) Get(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	u, err := s.userRepo.GetByID(ctx, id)
	if err != nil || u == nil {
		return nil, err
	}
	roles, perms, err := s.userRepo.GetUserRolesAndPermissions(ctx, id)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	u.Permissions = perms
	return u, nil
}

func (s *UserService) Create(ctx context.Context, req CreateUserRequest) (*domain.User, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		return nil, errors.New("a valid email is required")
	}
	if len(req.Password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}
	if existing, _ := s.userRepo.GetByEmail(ctx, tenantID, req.Email); existing != nil {
		return nil, errors.New("a user with this email already exists")
	}
	if req.Status == "" {
		req.Status = "active"
	}

	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	user := &domain.User{
		TenantID:     tenantID,
		Email:        req.Email,
		PasswordHash: hash,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		Status:       req.Status,
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}
	if len(req.RoleIDs) > 0 {
		if err := s.userRepo.SetRoles(ctx, user.ID, req.RoleIDs); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, user.ID)
}

func (s *UserService) Update(ctx context.Context, id uuid.UUID, req UpdateUserRequest) (*domain.User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	if req.Email != nil {
		user.Email = strings.TrimSpace(strings.ToLower(*req.Email))
	}
	if req.FirstName != nil {
		user.FirstName = *req.FirstName
	}
	if req.LastName != nil {
		user.LastName = *req.LastName
	}
	if req.Status != nil {
		user.Status = *req.Status
	}
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	if req.RoleIDs != nil {
		if err := s.userRepo.SetRoles(ctx, id, *req.RoleIDs); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

func (s *UserService) Delete(ctx context.Context, id uuid.UUID) error {
	actor, _ := contextutil.GetUserID(ctx)
	if actor == id {
		return errors.New("you cannot delete your own account")
	}
	return s.userRepo.Delete(ctx, id)
}

func (s *UserService) ResetPassword(ctx context.Context, id uuid.UUID, newPassword string) error {
	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := crypto.HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.userRepo.UpdatePassword(ctx, id, hash)
}
