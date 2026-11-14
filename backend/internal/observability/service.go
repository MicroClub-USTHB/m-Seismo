package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// ProviderConnection represents a configured observability provider connection for an app.
type ProviderConnection struct {
	Provider  Provider        `json:"provider"`
	Settings  json.RawMessage `json:"settings"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Service manages app observability configurations.
type Service struct {
	q db.Querier
}

// NewService creates a new observability service.
func NewService(q db.Querier) *Service {
	return &Service{q: q}
}

// GetConfigForApp aggregates all configured observability providers for an app
// and returns an aggregate Config for the worker pipeline.
func (s *Service) GetConfigForApp(ctx context.Context, appID uuid.UUID) (Config, error) {
	rows, err := s.q.ListAppObservabilityProviders(ctx, appID)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Window: DefaultWindow,
	}

	for _, row := range rows {
		switch Provider(row.Provider) {
		case ProviderGrafana:
			var g GrafanaConfig
			if err := json.Unmarshal(row.Settings, &g); err != nil {
				return Config{}, fmt.Errorf("unmarshal grafana settings: %w", err)
			}
			cfg.Grafana = &g
		case ProviderLoki:
			var l LokiConfig
			if err := json.Unmarshal(row.Settings, &l); err != nil {
				return Config{}, fmt.Errorf("unmarshal loki settings: %w", err)
			}
			cfg.Loki = &l
		case ProviderCloudWatch:
			var cw CloudWatchConfig
			if err := json.Unmarshal(row.Settings, &cw); err != nil {
				return Config{}, fmt.Errorf("unmarshal cloudwatch settings: %w", err)
			}
			cfg.CloudWatch = &cw
		case ProviderDatadog:
			var dd DatadogConfig
			if err := json.Unmarshal(row.Settings, &dd); err != nil {
				return Config{}, fmt.Errorf("unmarshal datadog settings: %w", err)
			}
			cfg.Datadog = &dd
		}
	}

	return cfg, nil
}

// List returns all configured observability provider connections for an app,
// ensuring the caller owns the app.
func (s *Service) List(ctx context.Context, ownerID, appID uuid.UUID) ([]ProviderConnection, error) {
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return nil, err
	}

	rows, err := s.q.ListAppObservabilityProviders(ctx, appID)
	if err != nil {
		return nil, err
	}

	out := make([]ProviderConnection, 0, len(rows))
	for _, r := range rows {
		out = append(out, ProviderConnection{
			Provider:  Provider(r.Provider),
			Settings:  json.RawMessage(r.Settings),
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

// Get returns the configuration for a specific observability provider for an app.
func (s *Service) Get(ctx context.Context, ownerID, appID uuid.UUID, provider Provider) (ProviderConnection, error) {
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return ProviderConnection{}, err
	}

	row, err := s.q.GetAppObservabilityProvider(ctx, db.GetAppObservabilityProviderParams{
		AppID:    appID,
		Provider: string(provider),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProviderConnection{}, apperr.NotFound(fmt.Sprintf("observability provider %q not configured", provider))
		}
		return ProviderConnection{}, err
	}

	return ProviderConnection{
		Provider:  Provider(row.Provider),
		Settings:  json.RawMessage(row.Settings),
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// Set creates or updates the configuration for an observability provider.
func (s *Service) Set(ctx context.Context, ownerID, appID uuid.UUID, provider Provider, settings json.RawMessage) error {
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return err
	}

	if err := validateSettings(provider, settings); err != nil {
		return err
	}

	_, err := s.q.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID:    appID,
		Provider: string(provider),
		Settings: settings,
	})
	return err
}

// Delete removes an observability provider configuration from an app.
func (s *Service) Delete(ctx context.Context, ownerID, appID uuid.UUID, provider Provider) error {
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return err
	}

	return s.q.DeleteAppObservabilityProvider(ctx, db.DeleteAppObservabilityProviderParams{
		AppID:    appID,
		Provider: string(provider),
	})
}

// DeleteAll removes all observability provider configurations from an app.
func (s *Service) DeleteAll(ctx context.Context, ownerID, appID uuid.UUID) error {
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return err
	}

	return s.q.DeleteAllAppObservabilityProviders(ctx, appID)
}

func (s *Service) requireApp(ctx context.Context, ownerID, appID uuid.UUID) error {
	_, err := s.q.GetApp(ctx, db.GetAppParams{ID: appID, OwnerID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound(fmt.Sprintf("app %s not found", appID))
		}
		return err
	}
	return nil
}

func validateSettings(provider Provider, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] != '{' {
		return apperr.Invalid("settings must be a valid JSON object")
	}

	switch provider {
	case ProviderGrafana:
		var cfg GrafanaConfig
		if err := json.Unmarshal(trimmed, &cfg); err != nil {
			return apperr.Invalid("invalid grafana settings: " + err.Error())
		}
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return apperr.Invalid("grafana base_url is required")
		}
		return nil

	case ProviderLoki:
		var cfg LokiConfig
		if err := json.Unmarshal(trimmed, &cfg); err != nil {
			return apperr.Invalid("invalid loki settings: " + err.Error())
		}
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return apperr.Invalid("loki base_url is required")
		}
		return nil

	case ProviderCloudWatch:
		var cfg CloudWatchConfig
		if err := json.Unmarshal(trimmed, &cfg); err != nil {
			return apperr.Invalid("invalid cloudwatch settings: " + err.Error())
		}
		if strings.TrimSpace(cfg.LogGroup) == "" {
			return apperr.Invalid("cloudwatch log_group is required")
		}
		return nil

	case ProviderDatadog:
		var cfg DatadogConfig
		if err := json.Unmarshal(trimmed, &cfg); err != nil {
			return apperr.Invalid("invalid datadog settings: " + err.Error())
		}
		return nil

	default:
		return apperr.Invalid(fmt.Sprintf("unsupported observability provider %q", provider))
	}
}
