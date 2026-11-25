// Package crashprovider links reports to an app's external crash service.
// It is separate from provider, which manages issue trackers such as GitHub.
package crashprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
)

var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)
var eventPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
var groupPattern = regexp.MustCompile(`^[0-9]+$`)

// SentrySettings never contains credentials. The operator explicitly allows
// origins (including any self-hosted instance); owners select one of them.
type SentrySettings struct {
	BaseURL      string `json:"base_url"`
	Organization string `json:"organization"`
	Project      string `json:"project"`
}

type SentryInput struct {
	SentrySettings
	AuthToken string `json:"auth_token"`
}

type sentryClient struct {
	http    *http.Client
	origins map[string]bool
}

func newSentryClient(origins []string) (*sentryClient, error) {
	c := &sentryClient{
		http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		origins: map[string]bool{},
	}
	for _, raw := range origins {
		origin, err := normalizeOrigin(raw)
		if err != nil {
			return nil, fmt.Errorf("crash_providers.sentry_origins: %w", err)
		}
		c.origins[origin] = true
	}
	return c, nil
}

func normalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("sentry base_url must be an HTTPS origin without a path, credentials, query or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

func (c *sentryClient) validate(s *SentrySettings) error {
	if s.BaseURL == "" {
		s.BaseURL = "https://sentry.io"
	}
	origin, err := normalizeOrigin(s.BaseURL)
	if err != nil {
		return apperr.Invalid(err.Error())
	}
	if !c.origins[origin] {
		return apperr.Invalid("Sentry origin is not allowed by this deployment")
	}
	s.BaseURL = origin
	if !slugPattern.MatchString(s.Organization) || !slugPattern.MatchString(s.Project) {
		return apperr.Invalid("Sentry organization and project must be valid slugs")
	}
	return nil
}

func (c *sentryClient) get(ctx context.Context, s SentrySettings, token, suffix string, out any) error {
	// Revalidate saved settings too: removing an origin from the operator's
	// allowlist must also stop existing connections from using it.
	if err := c.validate(&s); err != nil {
		return err
	}
	endpoint := s.BaseURL + "/api/0/projects/" + s.Organization + "/" + s.Project + "/" + suffix
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build Sentry request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// Do not propagate request headers, tokens, or upstream bodies.
		return apperr.Unavailable("Sentry is unavailable; retry later")
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return apperr.NotFound("Sentry project or event not found")
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return apperr.Invalid("Sentry rejected the connection; check the token and project:read permission")
	case http.StatusTooManyRequests:
		wait := 60 * time.Second
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 && seconds <= 86400 {
			wait = time.Duration(seconds) * time.Second
		} else if deadline, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && time.Until(deadline) > 0 {
			wait = min(time.Until(deadline), 24*time.Hour)
		}
		return apperr.RateLimited("Sentry rate limit reached; retry later", wait)
	default:
		return apperr.Unavailable("Sentry is unavailable; retry later")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return apperr.Unavailable("Sentry returned an unreadable event")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return apperr.Unavailable("Sentry returned an unreadable event")
	}
	return nil
}

// Event is an intentionally small projection of Sentry's event response.
// User identities, request headers/bodies, arbitrary contexts and breadcrumbs
// are not copied into m-Seismo or returned to clients.
type Event struct {
	Provider   string   `json:"provider"`
	EventID    string   `json:"event_id"`
	IssueID    string   `json:"issue_id,omitempty"`
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	StackTrace []string `json:"stack_trace,omitempty"`
}

type sentryEvent struct {
	EventID string `json:"eventID"`
	GroupID string `json:"groupID"`
	Title   string `json:"title"`
	Entries []struct {
		Type string `json:"type"`
		Data struct {
			Values []struct {
				Stacktrace struct {
					Frames []struct {
						Function string `json:"function"`
						Filename string `json:"filename"`
						LineNo   *int   `json:"lineNo"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"data"`
	} `json:"entries"`
}

func (c *sentryClient) event(ctx context.Context, s SentrySettings, token, id string) (*Event, error) {
	if !eventPattern.MatchString(id) {
		return nil, apperr.Invalid("invalid Sentry event ID")
	}
	id = strings.ToLower(id)
	var raw sentryEvent
	if err := c.get(ctx, s, token, "events/"+id+"/", &raw); err != nil {
		return nil, err
	}
	if !strings.EqualFold(raw.EventID, id) {
		return nil, apperr.Unavailable("Sentry returned an unexpected event")
	}
	e := &Event{Provider: "sentry", EventID: id, Title: truncate(raw.Title, 200)}
	if groupPattern.MatchString(raw.GroupID) {
		e.IssueID = raw.GroupID
		e.URL = s.BaseURL + "/organizations/" + s.Organization + "/issues/" + raw.GroupID + "/events/" + id + "/"
	} else {
		e.URL = s.BaseURL + "/organizations/" + s.Organization + "/issues/?query=" + id
	}
	for _, entry := range raw.Entries {
		if entry.Type != "exception" {
			continue
		}
		for _, value := range entry.Data.Values {
			// Sentry frames are oldest first; m-Seismo uses innermost first.
			frames := value.Stacktrace.Frames
			for i := len(frames) - 1; i >= 0 && len(e.StackTrace) < 512; i-- {
				f := frames[i]
				line := f.Function + " (" + f.Filename
				if f.LineNo != nil {
					line += ":" + strconv.Itoa(*f.LineNo)
				}
				e.StackTrace = append(e.StackTrace, truncate(line+")", 1000))
			}
		}
	}
	return e, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
