package stats

import (
	"context"
	"time"

	"university-result-processing/backend/internal/domain"
)

type StatsService struct {
	repo domain.StatsRepository
}

func NewStatsService(repo domain.StatsRepository) *StatsService {
	return &StatsService{repo: repo}
}

func (s *StatsService) Overview(ctx context.Context) (*domain.Overview, error) {
	return s.repo.Overview(ctx)
}

func (s *StatsService) Presence(ctx context.Context) ([]*domain.PresenceRow, error) {
	return s.repo.Presence(ctx)
}

func (s *StatsService) RecentActivity(ctx context.Context, limit int) ([]*domain.VerificationEvent, error) {
	return s.repo.RecentActivity(ctx, limit)
}

func (s *StatsService) Productivity(ctx context.Context, from, to time.Time) ([]*domain.ProductivityRow, error) {
	return s.repo.Productivity(ctx, from, to)
}

func (s *StatsService) Timeseries(ctx context.Context, from, to time.Time) ([]*domain.TimeseriesPoint, error) {
	return s.repo.Timeseries(ctx, from, to)
}
