// Package crashlytics is the crash.Source backed by the Crashlytics REST API
// (firebasecrashlytics.googleapis.com, v1alpha).
//
// The API is alpha: it has no stability guarantee and refuses service
// account credentials, so it is called as a Google user. Each m-Seismo
// project connects its own Google account through the OAuth flow in
// oauth.go; the refresh token is stored encrypted in the database (see
// Store). See backend/docs/crashlytics.md for the setup.
//
// events.list only accepts requests for one issue, and the installation ID
// it returns is not one the app can read on the device. So the app sets its
// m-Seismo installation ID as a Crashlytics custom key (InstallationKey), and
// Find lists the issues seen in the query's time window, then each issue's
// events carrying that key, and keeps the one closest in time.
package crashlytics

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	fc "google.golang.org/api/firebasecrashlytics/v1alpha"
	"google.golang.org/api/option"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash"
)

// InstallationKey is the Crashlytics custom key the app sets to its m-Seismo
// installation ID, so a report can be matched to its crash.
const InstallationKey = "seismo_installation_id"

const (
	// pageSize is the number of issues or events requested per call.
	pageSize = 100
	// maxPages caps how many pages Find reads for one list, so a busy app
	// cannot turn one lookup into an unbounded number of API calls.
	maxPages = 10
)

// Config identifies m-Seismo to Google as an OAuth client. It is the same
// for every project; each project's own access is a refresh token in Store.
type Config struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the public URL of the OAuth callback,
	// e.g. https://seismo.example.com/api/v1/oauth/crashlytics/callback.
	RedirectURL string
	// QuotaProject is the Google Cloud project billed for API quota. User
	// credentials need one.
	QuotaProject string
}

func (c Config) oauth() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Endpoint:     google.Endpoint,
		Scopes:       []string{fc.CloudPlatformScope, fc.FirebaseScope, "openid", "email"},
	}
}

// Source finds crashes through the Crashlytics REST API, with the
// credentials of the report's project.
type Source struct {
	oauth        *oauth2.Config
	quotaProject string
	store        Store
	opts         []option.ClientOption

	mu      sync.Mutex
	clients map[string]projectClient // by project ID
}

// projectClient is an API client for one project, kept while the
// project's refresh token does not change.
type projectClient struct {
	refreshToken string
	apps         *fc.ProjectsAppsService
}

var _ crash.Source = (*Source)(nil)

// New builds a Source. opts are appended to every API client's options,
// which lets tests point the clients at a fake server.
func New(cfg Config, store Store, opts ...option.ClientOption) (*Source, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("crashlytics: OAuth client ID and secret are required")
	}
	return &Source{
		oauth:        cfg.oauth(),
		quotaProject: cfg.QuotaProject,
		store:        store,
		opts:         opts,
		clients:      make(map[string]projectClient),
	}, nil
}

// Check reads the top issues of each app of each connected project, which
// proves the stored credentials are valid and can read those apps.
func (s *Source) Check(ctx context.Context) error {
	conns, err := s.store.List(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, conn := range conns {
		if len(conn.Apps) == 0 {
			errs = append(errs, fmt.Errorf("crashlytics: project %s has no Firebase apps configured", conn.ProjectID))
			continue
		}
		apps, err := s.appsFor(conn)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, app := range conn.Apps {
			if _, err := apps.Reports.Get(topIssues(app)).PageSize(1).Context(ctx).Do(); err != nil {
				errs = append(errs, fmt.Errorf("crashlytics: project %s: read %s: %w", conn.ProjectID, app, err))
			}
		}
	}
	return errors.Join(errs...)
}

// Find looks for the crash in q.App, or in every app of the project when
// q.App is empty. In each app it lists the issues seen between
// q.At-q.Window and q.At+q.Window, then each issue's events in that window
// whose InstallationKey is q.InstallationID, and keeps the one closest to
// q.At.
func (s *Source) Find(ctx context.Context, q crash.Query) (crash.Crash, error) {
	if q.InstallationID == "" {
		return crash.Crash{}, crash.ErrNotFound
	}

	conn, err := s.store.Get(ctx, q.ProjectID)
	if errors.Is(err, ErrNotConnected) {
		return crash.Crash{}, crash.ErrNotFound
	}
	if err != nil {
		return crash.Crash{}, err
	}

	apps := conn.Apps
	if q.App != "" {
		apps = []string{q.App}
	}

	svc, err := s.appsFor(conn)
	if err != nil {
		return crash.Crash{}, err
	}

	start := q.At.Add(-q.Window).UTC().Format(time.RFC3339Nano)
	end := q.At.Add(q.Window).UTC().Format(time.RFC3339Nano)
	// %q quotes the value as the filter syntax expects; installation IDs are
	// UUIDs, so it never needs escaping beyond that.
	filter := fmt.Sprintf("custom_keys.%s = %q", InstallationKey, q.InstallationID)

	var (
		best      *fc.Event
		bestTime  time.Time
		bestIssue *fc.Issue
	)
	for _, app := range apps {
		issues, err := issuesIn(ctx, svc, app, start, end)
		if err != nil {
			return crash.Crash{}, err
		}

		for _, issue := range issues {
			call := svc.Events.List(app).
				FilterIssueId(issue.Id).
				FilterIntervalStartTime(start).
				FilterIntervalEndTime(end).
				FilterExpression(filter).
				PageSize(pageSize).
				Context(ctx)

			for page, token := 0, ""; page < maxPages; page++ {
				resp, err := call.PageToken(token).Do()
				if err != nil {
					return crash.Crash{}, fmt.Errorf("crashlytics: list events of %s issue %s: %w", app, issue.Id, err)
				}

				for _, ev := range resp.Events {
					at, err := time.Parse(time.RFC3339Nano, ev.EventTime)
					if err != nil {
						continue
					}
					if best == nil || absDuration(at.Sub(q.At)) < absDuration(bestTime.Sub(q.At)) {
						best, bestTime, bestIssue = ev, at, issue
					}
				}

				token = resp.NextPageToken
				if token == "" {
					break
				}
			}
		}
	}

	if best == nil {
		return crash.Crash{}, crash.ErrNotFound
	}
	return toCrash(best, bestTime, bestIssue), nil
}

// issuesIn returns the issues of app that have events between start and end.
func issuesIn(ctx context.Context, svc *fc.ProjectsAppsService, app, start, end string) ([]*fc.Issue, error) {
	call := svc.Reports.Get(topIssues(app)).
		FilterIntervalStartTime(start).
		FilterIntervalEndTime(end).
		PageSize(pageSize).
		Context(ctx)

	var issues []*fc.Issue
	for page, token := 0, ""; page < maxPages; page++ {
		report, err := call.PageToken(token).Do()
		if err != nil {
			return nil, fmt.Errorf("crashlytics: list issues of %s: %w", app, err)
		}
		for _, g := range report.Groups {
			if g.Issue != nil && g.Issue.Id != "" {
				issues = append(issues, g.Issue)
			}
		}
		token = report.NextPageToken
		if token == "" {
			break
		}
	}
	return issues, nil
}

// topIssues is the name of app's report of issues ranked by event count.
func topIssues(app string) string {
	return app + "/reports/topIssues"
}

// appsFor returns the API client for conn's project, creating it when the
// project has none yet or its refresh token changed.
func (s *Source) appsFor(conn Connection) (*fc.ProjectsAppsService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.clients[conn.ProjectID]; ok && c.refreshToken == conn.RefreshToken {
		return c.apps, nil
	}

	// The token source outlives the request, so it gets its own context.
	ts := s.oauth.TokenSource(context.Background(), &oauth2.Token{RefreshToken: conn.RefreshToken})
	opts := []option.ClientOption{option.WithTokenSource(ts)}
	if s.quotaProject != "" {
		opts = append(opts, option.WithQuotaProject(s.quotaProject))
	}
	opts = append(opts, s.opts...)

	svc, err := fc.NewService(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("crashlytics: create client: %w", err)
	}

	s.clients[conn.ProjectID] = projectClient{refreshToken: conn.RefreshToken, apps: svc.Projects.Apps}
	return svc.Projects.Apps, nil
}

// toCrash builds the crash from ev. Events usually leave the issue's title
// and console URL empty, so they come from issue, the issue ev belongs to.
func toCrash(ev *fc.Event, at time.Time, issue *fc.Issue) crash.Crash {
	c := crash.Crash{
		ID:         ev.Name,
		Title:      ev.IssueTitle,
		Subtitle:   ev.IssueSubtitle,
		At:         at,
		Trace:      formatTrace(ev),
		CustomKeys: ev.CustomKeys,
	}
	if ev.Issue != nil {
		c.URL = ev.Issue.Uri
	}
	if issue != nil {
		c.Title = cmp.Or(c.Title, issue.Title)
		c.Subtitle = cmp.Or(c.Subtitle, issue.Subtitle)
		c.URL = cmp.Or(c.URL, issue.Uri)
	}
	for _, b := range ev.Breadcrumbs {
		bAt, _ := time.Parse(time.RFC3339Nano, b.EventTime)
		c.Breadcrumbs = append(c.Breadcrumbs, crash.Breadcrumb{At: bAt, Title: b.Title, Params: b.Params})
	}
	return c
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
