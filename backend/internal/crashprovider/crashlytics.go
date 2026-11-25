package crashprovider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// defaultCrashWindow is how far apart a report and its Crashlytics crash may
// be when SEISMO_CRASH_WINDOW is not set.
const defaultCrashWindow = 2 * time.Minute

// newModuleService is NewService plus the deployment's crash source, which
// resolves apps whose provider is Crashlytics.
func newModuleService(q db.Querier, cfg *config.Config, src crash.Source) (*Service, error) {
	s, err := NewService(q, cfg)
	if err != nil {
		return nil, err
	}
	s.crashes = src
	s.crashWindow = cfg.Crash.Window
	return s, nil
}

// resolveCrashlytics finds the Crashlytics crash of report. Crashlytics is
// connected per m-Seismo project, so each project the app belongs to is
// tried until one has the crash. saved is the app's Crashlytics choice, or
// nil when the app has none: then every Firebase app of the project's
// connection is searched. No match is not an error: the issue is published
// without crash details.
func (s *Service) resolveCrashlytics(ctx context.Context, appID uuid.UUID, saved *db.AppCrashProvider, report *ingest.Report) (*Event, error) {
	if s.crashes == nil || report.InstallationID == "" {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339Nano, report.OccurredAt)
	if err != nil {
		return nil, nil
	}

	app := ""
	if saved != nil {
		var settings CrashlyticsSettings
		if err := json.Unmarshal(saved.Settings, &settings); err != nil {
			return nil, err
		}
		if settings.ProjectID != "" && settings.AppID != "" {
			app = "projects/" + settings.ProjectID + "/apps/" + settings.AppID
		}
	}

	projects, err := s.q.ListAppProjects(ctx, appID)
	if err != nil {
		return nil, err
	}
	window := s.crashWindow
	if window <= 0 {
		window = defaultCrashWindow
	}

	var errs []error
	for _, p := range projects {
		c, err := s.crashes.Find(ctx, crash.Query{
			ProjectID:      p.ID.String(),
			App:            app,
			InstallationID: report.InstallationID,
			At:             at,
			Window:         window,
		})
		if errors.Is(err, crash.ErrNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return crashlyticsEvent(c), nil
	}
	return nil, errors.Join(errs...)
}

func crashlyticsEvent(c crash.Crash) *Event {
	title := c.Title
	if c.Subtitle != "" {
		title = strings.TrimSpace(title + " — " + c.Subtitle)
	}
	var trace []string
	for _, line := range strings.Split(c.Trace, "\n") {
		if line = strings.TrimRight(line, " \t"); line != "" {
			trace = append(trace, line)
		}
	}
	return &Event{
		Provider:   "crashlytics",
		EventID:    eventIDOf(c.ID),
		URL:        c.URL,
		Title:      title,
		StackTrace: trace,
	}
}

// eventIDOf keeps the last segment of a Crashlytics event resource name
// (projects/…/apps/…/events/<id>).
func eventIDOf(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
