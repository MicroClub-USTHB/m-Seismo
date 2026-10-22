// Package gitlab connects apps to GitLab (gitlab.com or self-managed) with
// OAuth 2.0 and opens issues through the GitLab REST API v4. It is the only
// package that talks to GitLab.
//
// Access model: each app runs its own OAuth authorization (scope "api"),
// so each app's GitLab connection carries its own token, stored encrypted
// per connection (provider.SecretStore). The connection's external ID is
// the GitLab user ID; issues are authored by that GitLab user. Access
// tokens last two hours and are refreshed automatically. GitLab rotates the
// refresh token on every refresh, and the new one is saved.
package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/oauth"
)

// Module registers the GitLab provider. It is always available: each app
// registers its own OAuth application (client ID and secret) before it can
// connect, through provider.CredentialStore, instead of the deployment
// sharing one configured in gitlab.client_id/client_secret.
var Module = fx.Module("gitlab",
	fx.Provide(provider.AsProvider(FromConfig)),
)

// FromConfig builds the provider from the deployment-wide GitLab instance
// settings (self-managed URL, rate limits). Per-app client ID and secret
// come from creds at connect time.
func FromConfig(cfg *config.Config, store provider.SecretStore, creds provider.CredentialStore) ([]provider.Provider, error) {
	c := cfg.GitLab
	p, err := New(Options{
		WebURL:           c.WebURL,
		APIURL:           c.APIURL,
		RedirectURL:      c.RedirectURL,
		MaxRateLimitWait: c.MaxRateLimitWait,
	}, store, creds)
	if err != nil {
		return nil, err
	}
	return []provider.Provider{p}, nil
}

type Options struct {
	// WebURL is the GitLab instance, e.g. https://gitlab.com/.
	WebURL string
	// APIURL is the REST API v4 base, e.g. https://gitlab.com/api/v4/.
	APIURL string
	// RedirectURL is the OAuth callback registered on every app's GitLab
	// application: {public_url}/api/v1/providers/gitlab/callback.
	RedirectURL string
	// MaxRateLimitWait: see oauth.Client.
	MaxRateLimitWait time.Duration
	// PageSize is how many projects are listed per request (default 100).
	PageSize int
}

// Provider implements provider.Provider for GitLab.
type Provider struct {
	opts   Options
	creds  provider.CredentialStore
	tokens *oauth.Tokens
	api    *oauth.Client
}

func New(o Options, store provider.SecretStore, creds provider.CredentialStore) (*Provider, error) {
	if o.WebURL == "" {
		o.WebURL = "https://gitlab.com/"
	}
	o.WebURL = withSlash(o.WebURL)
	if o.APIURL == "" {
		o.APIURL = o.WebURL + "api/v4/"
	}
	o.APIURL = withSlash(o.APIURL)
	if o.PageSize <= 0 {
		o.PageSize = 100
	}

	p := &Provider{
		opts:  o,
		creds: creds,
		api: &oauth.Client{
			Tracker:          "gitlab",
			BaseURL:          o.APIURL,
			HTTP:             &http.Client{Timeout: 30 * time.Second},
			MaxRateLimitWait: o.MaxRateLimitWait,
		},
	}
	p.tokens = oauth.NewTokens(store, p.endpointFor)
	return p, nil
}

func (p *Provider) Tracker() db.Tracker { return db.TrackerGitlab }

// endpointFor builds the OAuth application of the app's own GitLab
// application, resolved from provider.CredentialStore.
func (p *Provider) endpointFor(ctx context.Context, appID uuid.UUID) (oauth.Endpoint, error) {
	cred, err := p.creds.Get(ctx, appID, db.TrackerGitlab)
	if errors.Is(err, provider.ErrNoCredentials) {
		return oauth.Endpoint{}, apperr.NotFound("this app has no GitLab OAuth application yet; register one with PUT /apps/{appID}/providers/gitlab/credentials")
	}
	if err != nil {
		return oauth.Endpoint{}, err
	}
	return oauth.Endpoint{
		Tracker:      "gitlab",
		AuthURL:      p.opts.WebURL + "oauth/authorize",
		TokenURL:     p.opts.WebURL + "oauth/token",
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
		RedirectURL:  p.opts.RedirectURL,
		// "api" is the narrowest GitLab scope that can create issues.
		Scopes: []string{"api"},
		HTTP:   &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// InstallURL is GitLab's OAuth authorize page, for the app's own GitLab
// OAuth application.
func (p *Provider) InstallURL(ctx context.Context, appID uuid.UUID, state string) (string, error) {
	endpoint, err := p.endpointFor(ctx, appID)
	if err != nil {
		return "", err
	}
	return endpoint.AuthCodeURL(state), nil
}

// CompleteInstall exchanges the code GitLab sent to the callback (the code
// can only be redeemed with the app's own GitLab application secret, so it
// proves the grant) and reads the authorising user. The token becomes the
// connection's secret.
func (p *Provider) CompleteInstall(ctx context.Context, appID uuid.UUID, q url.Values) (provider.Account, error) {
	if e := q.Get("error"); e != "" {
		msg := e
		if d := q.Get("error_description"); d != "" {
			msg = d
		}
		return provider.Account{}, apperr.Invalid("gitlab authorization was not granted: " + msg)
	}
	code := q.Get("code")
	if code == "" {
		return provider.Account{}, apperr.Invalid("missing authorization code")
	}
	endpoint, err := p.endpointFor(ctx, appID)
	if err != nil {
		return provider.Account{}, err
	}
	tok, err := endpoint.Exchange(ctx, code)
	if err != nil {
		return provider.Account{}, err
	}

	var u struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if _, err := p.api.Do(ctx, oauth.StaticToken(tok.AccessToken), http.MethodGet, "user", nil, &u); err != nil {
		return provider.Account{}, fmt.Errorf("gitlab: read the authorizing user: %w", err)
	}
	if u.ID == 0 {
		return provider.Account{}, errors.New("gitlab: the user endpoint returned no id")
	}
	return provider.Account{
		ExternalID: strconv.FormatInt(u.ID, 10),
		Login:      u.Username,
		Type:       "User",
		Secret:     tok.Encode(),
	}, nil
}

// ListRepositories lists the projects the connection's user is a member of
// (at least Guest, which can open issues), following X-Next-Page.
func (p *Provider) ListRepositories(ctx context.Context, c issue.Connection) ([]provider.Repository, error) {
	auth := p.tokens.For(c.ID, c.AppID)
	out := []provider.Repository{}
	for page := "1"; page != ""; {
		q := url.Values{
			"membership":       {"true"},
			"min_access_level": {"10"},
			"order_by":         {"path"},
			"sort":             {"asc"},
			"per_page":         {strconv.Itoa(p.opts.PageSize)},
			"page":             {page},
		}
		var projects []struct {
			Path       string `json:"path_with_namespace"`
			Visibility string `json:"visibility"`
			WebURL     string `json:"web_url"`
		}
		h, err := p.api.Do(ctx, auth, http.MethodGet, "projects?"+q.Encode(), nil, &projects)
		if err != nil {
			return nil, fmt.Errorf("gitlab: list projects: %w", err)
		}
		for _, pr := range projects {
			out = append(out, provider.Repository{FullName: pr.Path, Private: pr.Visibility != "public", URL: pr.WebURL})
		}
		page = h.Get("X-Next-Page")
	}
	return out, nil
}

// HasRepository reports whether the connection's user can see the project.
func (p *Provider) HasRepository(ctx context.Context, c issue.Connection, repository string) (bool, error) {
	if err := p.ValidateRepository(repository); err != nil {
		return false, err
	}
	_, err := p.api.Do(ctx, p.tokens.For(c.ID, c.AppID), http.MethodGet, projectPath(repository), nil, nil)
	if oauth.IsStatus(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("gitlab: get project %s: %w", repository, err)
	}
	return true, nil
}

// pathPattern is a GitLab project path: a namespace (group, subgroups or
// user) and a project, each segment made of letters, digits, '.', '_' or
// '-' and not starting with '-' or '.'.
var pathPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*(/[A-Za-z0-9_][A-Za-z0-9_.-]*)+$`)

// ValidateRepository accepts group/project and group/subgroup/…/project.
func (p *Provider) ValidateRepository(repository string) error {
	if !pathPattern.MatchString(repository) || strings.Contains(repository, "..") {
		return apperr.Invalid("repository must be a GitLab project path, e.g. group/project or group/subgroup/project")
	}
	return nil
}

// Covers is true for any valid project path: an OAuth token reaches every
// project its user can see, which cannot be told without asking GitLab.
// With several GitLab connections on one app, the first one is used.
func (p *Provider) Covers(_ issue.Connection, repository string) bool {
	return p.ValidateRepository(repository) == nil
}

func (p *Provider) Opener(_ context.Context, c issue.Connection) (issue.Opener, error) {
	return &opener{p: p, auth: p.tokens.For(c.ID, c.AppID)}, nil
}

// projectPath is the API path of a project, addressed by its URL-encoded
// full path as GitLab allows ("group%2Fproject").
func projectPath(repository string) string {
	return "projects/" + url.PathEscape(repository)
}

func withSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

var _ provider.Provider = (*Provider)(nil)
