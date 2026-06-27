package auth

import (
	"context"
	"errors"
	"time"

	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/crypto"
)

type RegisterRequest struct {
	TenantName string `json:"tenant_name"`
	Domain     string `json:"domain"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
}

type LoginRequest struct {
	Domain   string `json:"domain"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token        string      `json:"token"`
	RefreshToken string      `json:"refresh_token"`
	User         domain.User `json:"user"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type AuthService struct {
	tenantRepo domain.TenantRepository
	userRepo   domain.UserRepository
	jwtSecret  string
}

func NewAuthService(tr domain.TenantRepository, ur domain.UserRepository, secret string) *AuthService {
	return &AuthService{
		tenantRepo: tr,
		userRepo:   ur,
		jwtSecret:  secret,
	}
}

func (s *AuthService) Register(ctx context.Context, req RegisterRequest) (*domain.Tenant, *domain.User, error) {
	// Create Tenant
	tenant := &domain.Tenant{
		Name:     req.TenantName,
		Domain:   req.Domain,
		Settings: map[string]interface{}{},
	}
	if err := s.tenantRepo.Create(ctx, tenant); err != nil {
		return nil, nil, err
	}

	// Hash Password
	passHash, err := crypto.HashPassword(req.Password)
	if err != nil {
		return nil, nil, err
	}

	// Create User
	user := &domain.User{
		TenantID:     tenant.ID,
		Email:        req.Email,
		PasswordHash: passHash,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		Status:       "active",
	}

	// Create transaction/save user
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, nil, err
	}

	// Assign Evaluator/Admin roles initially
	if err := s.userRepo.AssignRole(ctx, user.ID, "System Admin"); err != nil {
		// Log and continue, role could be inserted via migrations seeding
	}

	return tenant, user, nil
}

func (s *AuthService) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// Find Tenant by domain
	tenant, err := s.tenantRepo.GetByDomain(ctx, req.Domain)
	if err != nil || tenant == nil {
		return nil, errors.New("invalid tenant domain or credentials")
	}

	// Get User by email inside tenant
	user, err := s.userRepo.GetByEmail(ctx, tenant.ID, req.Email)
	if err != nil || user == nil {
		return nil, errors.New("invalid tenant domain or credentials")
	}

	if user.Status != "active" {
		return nil, errors.New("user account is inactive")
	}

	// Verify Password
	valid, err := crypto.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !valid {
		return nil, errors.New("invalid tenant domain or credentials")
	}

	// Get roles and permissions
	roles, perms, err := s.userRepo.GetUserRolesAndPermissions(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	user.Roles = roles
	user.Permissions = perms

	// Generate JWT
	token, err := crypto.GenerateToken(user.ID, tenant.ID, roles, s.jwtSecret, 24*time.Hour)
	if err != nil {
		return nil, err
	}

	refreshToken, err := crypto.GenerateToken(user.ID, tenant.ID, []string{"refresh"}, s.jwtSecret, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		User:         *user,
	}, nil
}

// Refresh exchanges a valid, non-expired refresh token for a fresh access
// token (and a rotated refresh token). It re-reads the user's current status,
// roles and permissions so that a revoked/suspended account or changed
// permissions take effect on the next refresh rather than living on for the
// full refresh-token lifetime.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	claims, err := crypto.ParseToken(refreshToken, s.jwtSecret)
	if err != nil {
		return nil, errors.New("invalid or expired refresh token")
	}

	// Guard against an access token being replayed here: refresh tokens are
	// minted with the sentinel "refresh" role.
	isRefresh := false
	for _, r := range claims.Roles {
		if r == "refresh" {
			isRefresh = true
			break
		}
	}
	if !isRefresh {
		return nil, errors.New("not a refresh token")
	}

	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil || user == nil {
		return nil, errors.New("user not found")
	}
	if user.Status != "active" {
		return nil, errors.New("user account is inactive")
	}

	roles, perms, err := s.userRepo.GetUserRolesAndPermissions(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	user.Roles = roles
	user.Permissions = perms

	token, err := crypto.GenerateToken(user.ID, user.TenantID, roles, s.jwtSecret, 24*time.Hour)
	if err != nil {
		return nil, err
	}

	newRefreshToken, err := crypto.GenerateToken(user.ID, user.TenantID, []string{"refresh"}, s.jwtSecret, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		Token:        token,
		RefreshToken: newRefreshToken,
		User:         *user,
	}, nil
}
