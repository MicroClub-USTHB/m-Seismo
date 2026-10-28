package crashprovider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// CrashlyticsSettings preserves the per-app choice for the future Firebase
// export adapter. Selecting it does not require or use a Sentry credential.
type CrashlyticsSettings struct {
	ProjectID string `json:"project_id"`
	AppID     string `json:"app_id"`
}

type Input struct {
	Provider    string               `json:"provider"`
	Sentry      *SentryInput         `json:"sentry,omitempty"`
	Crashlytics *CrashlyticsSettings `json:"crashlytics,omitempty"`
}

type Connection struct {
	Provider       string               `json:"provider"`
	Sentry         *SentrySettings      `json:"sentry,omitempty"`
	Crashlytics    *CrashlyticsSettings `json:"crashlytics,omitempty"`
	HasCredentials bool                 `json:"has_credentials"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

type Link struct {
	Status   string `json:"status"`
	Provider string `json:"provider,omitempty"`
	Event    *Event `json:"event,omitempty"`
}

type Service struct {
	q      db.Querier
	vault  *vault
	sentry *sentryClient
}

func NewService(q db.Querier, cfg *config.Config) (*Service, error) {
	v, err := newVault(cfg.CrashProviders.EncryptionKey, cfg.CrashProviders.PreviousEncryptionKeys...)
	if err != nil {
		return nil, err
	}
	c, err := newSentryClient(cfg.CrashProviders.SentryOrigins)
	if err != nil {
		return nil, err
	}
	return &Service{q: q, vault: v, sentry: c}, nil
}

func (s *Service) own(ctx context.Context, ownerID, appID uuid.UUID) error {
	_, err := s.q.GetApp(ctx, db.GetAppParams{ID: appID, OwnerID: ownerID})
	return err
}

func (s *Service) Get(ctx context.Context, ownerID, appID uuid.UUID) (Connection, error) {
	if err := s.own(ctx, ownerID, appID); err != nil {
		return Connection{}, err
	}
	row, err := s.q.GetAppCrashProvider(ctx, appID)
	if err != nil {
		return Connection{}, err
	}
	return connection(row)
}

func connection(row db.AppCrashProvider) (Connection, error) {
	c := Connection{Provider: row.Provider, HasCredentials: len(row.Credentials) > 0, UpdatedAt: row.UpdatedAt}
	var err error
	if row.Provider == "sentry" {
		c.Sentry = &SentrySettings{}
		err = json.Unmarshal(row.Settings, c.Sentry)
	} else {
		c.Crashlytics = &CrashlyticsSettings{}
		err = json.Unmarshal(row.Settings, c.Crashlytics)
	}
	return c, err
}

func (s *Service) Set(ctx context.Context, ownerID, appID uuid.UUID, in Input) (Connection, error) {
	if err := s.own(ctx, ownerID, appID); err != nil {
		return Connection{}, err
	}
	params := db.SetAppCrashProviderParams{AppID: appID, Provider: in.Provider}
	switch in.Provider {
	case "sentry":
		if in.Sentry == nil || in.Crashlytics != nil {
			return Connection{}, apperr.Invalid("provide sentry settings only")
		}
		settings := in.Sentry.SentrySettings
		if err := s.sentry.validate(&settings); err != nil {
			return Connection{}, err
		}
		if s.vault.AEAD == nil {
			return Connection{}, apperr.Unavailable("Sentry connections require the operator to configure credential encryption")
		}
		token := strings.TrimSpace(in.Sentry.AuthToken)
		if token == "" {
			previous, err := s.q.GetAppCrashProvider(ctx, appID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Connection{}, apperr.Invalid("auth_token is required for a new Sentry connection")
			}
			if err != nil {
				return Connection{}, err
			}
			old, err := connection(previous)
			if err != nil {
				return Connection{}, err
			}
			if old.Sentry == nil || old.Sentry.BaseURL != settings.BaseURL {
				return Connection{}, apperr.Invalid("auth_token is required when changing Sentry origin or provider")
			}
			token, err = s.vault.open(appID, previous.Credentials)
			if err != nil {
				return Connection{}, err
			}
		}
		if len(token) > 4096 || strings.ContainsAny(token, "\r\n\t ") {
			return Connection{}, apperr.Invalid("invalid Sentry auth_token")
		}
		// Verify access before replacing the saved connection. A failed
		// update leaves the previously working provider intact.
		var project struct {
			ID string `json:"id"`
		}
		if err := s.sentry.get(ctx, settings, token, "", &project); err != nil {
			return Connection{}, err
		}
		if project.ID == "" {
			return Connection{}, apperr.Unavailable("Sentry returned an unreadable project")
		}
		var err error
		params.Credentials, err = s.vault.seal(appID, token)
		if err != nil {
			return Connection{}, err
		}
		params.Settings, _ = json.Marshal(settings)
	case "crashlytics":
		if in.Crashlytics == nil || in.Sentry != nil {
			return Connection{}, apperr.Invalid("provide crashlytics settings only")
		}
		settings := *in.Crashlytics
		settings.ProjectID = strings.TrimSpace(settings.ProjectID)
		settings.AppID = strings.TrimSpace(settings.AppID)
		if !slugPattern.MatchString(settings.ProjectID) || settings.AppID == "" || len(settings.AppID) > 255 {
			return Connection{}, apperr.Invalid("Crashlytics project_id and app_id are required")
		}
		params.Settings, _ = json.Marshal(settings)
	default:
		return Connection{}, apperr.Invalid("provider must be sentry or crashlytics")
	}
	row, err := s.q.SetAppCrashProvider(ctx, params)
	if err != nil {
		return Connection{}, err
	}
	return connection(row)
}

func (s *Service) Delete(ctx context.Context, ownerID, appID uuid.UUID) error {
	if err := s.own(ctx, ownerID, appID); err != nil {
		return err
	}
	return s.q.DeleteAppCrashProvider(ctx, appID)
}

// ReportCrash resolves the exact event attached by the host app's existing
// Sentry SDK. It never searches across projects or guesses by timestamp.
// Resolution is lazy: an unavailable crash provider cannot block ingestion,
// and a report can resolve later once Sentry finishes processing the event.
func (s *Service) ReportCrash(ctx context.Context, ownerID, appID, reportID uuid.UUID) (Link, error) {
	if err := s.own(ctx, ownerID, appID); err != nil {
		return Link{}, err
	}
	row, err := s.q.GetReportByReportID(ctx, db.GetReportByReportIDParams{AppID: appID, ReportID: reportID})
	if err != nil {
		return Link{}, err
	}
	r, err := ingest.ParseReport(row.Payload)
	if err != nil {
		return Link{}, err
	}
	saved, err := s.q.GetAppCrashProvider(ctx, appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{Status: "not_configured"}, nil
	}
	if err != nil {
		return Link{}, err
	}
	if saved.Provider == "crashlytics" {
		return Link{Status: "adapter_pending", Provider: "crashlytics"}, nil
	}
	if r.Sentry == nil {
		return Link{Status: "no_reference", Provider: "sentry"}, nil
	}
	event, err := s.resolveSentry(ctx, saved, r.Sentry.EventID)
	if kind, _ := apperr.Classify(err); err != nil && kind == apperr.KindNotFound {
		return Link{Status: "event_not_found", Provider: "sentry"}, nil
	}
	if err != nil {
		return Link{}, err
	}
	return Link{Status: "linked", Provider: "sentry", Event: event}, nil
}

// ForReport enriches an already stored report for the internal worker. It is not
// an owner API: the worker supplies the app ID from the persisted report row.
// A validated reference is returned even when Sentry is processing the event or
// unavailable, so issue publication retains the exact event ID and a useful link.
func (s *Service) ForReport(ctx context.Context, appID uuid.UUID, report *ingest.Report) (*Event, error) {
	if report.Sentry == nil {
		return nil, nil
	}
	saved, err := s.q.GetAppCrashProvider(ctx, appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if saved.Provider != "sentry" {
		return nil, nil
	}
	return s.resolveSentry(ctx, saved, report.Sentry.EventID)
}

func (s *Service) resolveSentry(ctx context.Context, saved db.AppCrashProvider, eventID string) (*Event, error) {
	var settings SentrySettings
	if err := json.Unmarshal(saved.Settings, &settings); err != nil {
		return nil, err
	}
	if err := s.sentry.validate(&settings); err != nil {
		return nil, err
	}
	if !eventPattern.MatchString(eventID) {
		return nil, apperr.Invalid("invalid Sentry event ID")
	}
	eventID = strings.ToLower(eventID)
	reference := &Event{Provider: "sentry", EventID: eventID,
		URL: settings.BaseURL + "/organizations/" + settings.Organization + "/issues/?query=" + eventID}
	token, err := s.vault.open(saved.AppID, saved.Credentials)
	if err != nil {
		return reference, apperr.Unavailable("Sentry credentials cannot be decrypted; reconnect the app or check the encryption keys")
	}
	event, err := s.sentry.event(ctx, settings, token, eventID)
	if err != nil {
		return reference, err
	}
	return event, nil
}
