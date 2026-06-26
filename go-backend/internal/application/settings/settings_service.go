package settings

import (
	"context"
	"errors"

	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

// Defaults is the platform settings catalog with default values. Only keys
// present here are accepted from clients (unknown keys are ignored), and the
// effective settings are always defaults overlaid with stored overrides.
var Defaults = map[string]interface{}{
	"low_confidence_threshold":  0.85, // cells below this are flagged for review in the grid
	"flag_inferred_values":      true, // highlight consensus-inferred cells distinctly
	"export_include_confidence": true, // include confidence column in CSV/Excel exports
}

type SettingsResponse struct {
	OrganizationName string                 `json:"organization_name"`
	Domain           string                 `json:"domain"`
	Settings         map[string]interface{} `json:"settings"`
}

type UpdateRequest struct {
	OrganizationName *string                `json:"organization_name"`
	Settings         map[string]interface{} `json:"settings"`
}

type SettingsService struct {
	tenantRepo domain.TenantRepository
}

func NewSettingsService(tr domain.TenantRepository) *SettingsService {
	return &SettingsService{tenantRepo: tr}
}

func merged(stored map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(Defaults))
	for k, v := range Defaults {
		out[k] = v
	}
	for k, v := range stored {
		if _, known := Defaults[k]; known {
			out[k] = v
		}
	}
	return out
}

func (s *SettingsService) Get(ctx context.Context) (*SettingsResponse, error) {
	tid, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.tenantRepo.GetByID(ctx, tid)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("tenant not found")
	}
	return &SettingsResponse{OrganizationName: t.Name, Domain: t.Domain, Settings: merged(t.Settings)}, nil
}

func (s *SettingsService) Update(ctx context.Context, req UpdateRequest) (*SettingsResponse, error) {
	tid, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.tenantRepo.GetByID(ctx, tid)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("tenant not found")
	}
	if t.Settings == nil {
		t.Settings = map[string]interface{}{}
	}
	if req.OrganizationName != nil && *req.OrganizationName != "" {
		t.Name = *req.OrganizationName
	}
	// Accept only known keys.
	for k, v := range req.Settings {
		if _, ok := Defaults[k]; ok {
			t.Settings[k] = v
		}
	}
	// Clamp the confidence threshold to a sane 0..1 range.
	if v, ok := t.Settings["low_confidence_threshold"].(float64); ok {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		t.Settings["low_confidence_threshold"] = v
	}
	if err := s.tenantRepo.Update(ctx, t); err != nil {
		return nil, err
	}
	return &SettingsResponse{OrganizationName: t.Name, Domain: t.Domain, Settings: merged(t.Settings)}, nil
}
