package verification

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

// DocumentState bundles the verification state with the per-page checklist for
// the verifier UI.
type DocumentState struct {
	*domain.VerificationItem
	Pages []*domain.PageVerification `json:"pages"`
}

type VerificationService struct {
	repo domain.VerificationRepository
}

func NewVerificationService(repo domain.VerificationRepository) *VerificationService {
	return &VerificationService{repo: repo}
}

func (s *VerificationService) Queue(ctx context.Context, scope string) ([]*domain.VerificationItem, error) {
	userID, err := contextutil.GetUserID(ctx)
	if err != nil {
		return nil, err
	}
	if scope == "" {
		scope = "available"
	}
	return s.repo.ListQueue(ctx, scope, userID)
}

func (s *VerificationService) State(ctx context.Context, docID uuid.UUID) (*DocumentState, error) {
	item, err := s.repo.GetState(ctx, docID)
	if err != nil || item == nil {
		return nil, err
	}
	pages, err := s.repo.GetPageVerifications(ctx, docID)
	if err != nil {
		return nil, err
	}
	return &DocumentState{VerificationItem: item, Pages: pages}, nil
}

// Claim attempts a first-come lock. Returns the resulting state, or an error if
// the file was already taken.
func (s *VerificationService) Claim(ctx context.Context, docID uuid.UUID) (*DocumentState, error) {
	userID, err := contextutil.GetUserID(ctx)
	if err != nil {
		return nil, err
	}
	ok, err := s.repo.Claim(ctx, docID, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("this file is already locked by another verifier or is not available")
	}
	return s.State(ctx, docID)
}

func (s *VerificationService) Release(ctx context.Context, docID uuid.UUID, force bool) error {
	userID, err := contextutil.GetUserID(ctx)
	if err != nil {
		return err
	}
	return s.repo.Release(ctx, docID, userID, force)
}

func (s *VerificationService) Assign(ctx context.Context, docID uuid.UUID, assignee *uuid.UUID) error {
	return s.repo.Assign(ctx, docID, assignee)
}

func (s *VerificationService) SetPresence(ctx context.Context, docID uuid.UUID, page int) error {
	userID, err := contextutil.GetUserID(ctx)
	if err != nil {
		return err
	}
	return s.repo.SetPresence(ctx, docID, userID, page)
}

func (s *VerificationService) MarkPage(ctx context.Context, docID uuid.UUID, page int, verified bool) error {
	userID, err := contextutil.GetUserID(ctx)
	if err != nil {
		return err
	}
	return s.repo.MarkPage(ctx, docID, page, userID, verified)
}

func (s *VerificationService) Submit(ctx context.Context, docID uuid.UUID) error {
	userID, err := contextutil.GetUserID(ctx)
	if err != nil {
		return err
	}
	return s.repo.Submit(ctx, docID, userID)
}
