// Package bitbucket connects apps to Bitbucket Cloud with OAuth 2.0 and
// opens issues through the Bitbucket REST API 2.0. It is the only package
// that talks to Bitbucket.
//
// Access model: each app runs its own OAuth authorization against the
// m-Seismo OAuth consumer (permissions: Account read, Repositories read,
// Issues write), so each app's Bitbucket connection carries its own token,
// stored encrypted per connection (provider.SecretStore). The connection's
// external ID is the Bitbucket user UUID; issues are reported by that
// user. Access tokens last two hours and are refreshed automatically.
//
// Bitbucket issues have no labels: labels are listed at the end of the
// issue body instead. Assignees need Atlassian account IDs, which m-Seismo
// does not have, so they are skipped with a warning.
package bitbucket

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
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/oauth"
)

// Module registers the Bitbucket provider and its webhook. It is always
// available: each app registers its own OAuth consumer (key and secret)
// before it can connect, through provider.CredentialStore, instead of the
// deployment sharing one configured in bitbucket.client_id/client_secret.
var Module = fx.Module("bitbucket",
	fx.Provide(
		provider.AsProvider(FromConfig),
		func(q db.Querier) IssueStates { return q },
		func(q db.Querier) WebhookSecrets { return q },
		httpx.AsRoutes(NewWebhookHandler),
	),
)

// FromConfig builds the provider from the deployment-wide Bitbucket
// settings (API/web URLs, rate limits). Per-app consumer key and secret
// come from creds at connect time.
func FromConfig(cfg *config.Config, store provider.SecretStore, creds provider.CredentialStore) ([]provider.Provider, error) {
	c := cfg.Bitbucket
	p, err := New(Options{
		WebURL:           c.WebURL,
		APIURL:           c.APIURL,
		RedirectURL:      c.RedirectURL,
		MaxRateLimitWait: c.MaxRateLimitWait,
		WebhookURL:       strings.TrimRight(cfg.Server.PublicURL, "/") + "/api/v1/webhooks/bitbucket",
	}, store, creds)
	if err != nil {
		return nil, err
	}
	return []provider.Provider{p}, nil
}

type Options struct {
	// WebURL hosts the OAuth endpoints: https://bitbucket.org/.
	WebURL string
	// APIURL is the REST API 2.0 base: https://api.bitbucket.org/2.0/.
	APIURL string
	// RedirectURL is the consumer's callback URL, shared by every app:
	// {public_url}/api/v1/providers/bitbucket/callback.
	RedirectURL string
	// WebhookURL is where a repository's webhook delivers:
	// {public_url}/api/v1/webhooks/bitbucket.
	WebhookURL string
	// MaxRateLimitWait: see oauth.Client.
	MaxRateLimitWait time.Duration
	// PageSize is how many repositories are listed per request (default
	// 100, Bitbucket's maximum).
	PageSize int
}

// Provider implements provider.Provider for Bitbucket Cloud.
type Provider struct {
	opts   Options
	creds  provider.CredentialStore
	tokens *oauth.Tokens
	api    *oauth.Client
}

func New(o Options, store provider.SecretStore, creds provider.CredentialStore) (*Provider, error) {
	if o.WebURL == "" {
		o.WebURL = "https://bitbucket.org/"
	}
	o.WebURL = withSlash(o.WebURL)
	if o.APIURL == "" {
		o.APIURL = "https://api.bitbucket.org/2.0/"
	}
	o.APIURL = withSlash(o.APIURL)
	if o.PageSize <= 0 || o.PageSize > 100 {
		o.PageSize = 100
	}

	p := &Provider{
		opts:  o,
		creds: creds,
		api: &oauth.Client{
			Tracker:          "bitbucket",
			BaseURL:          o.APIURL,
			HTTP:             &http.Client{Timeout: 30 * time.Second},
			MaxRateLimitWait: o.MaxRateLimitWait,
		},
	}
	p.tokens = oauth.NewTokens(store, p.endpointFor)
	return p, nil
}

func (p *Provider) Tracker() db.Tracker { return db.TrackerBitbucket }

// endpointFor builds the OAuth application of the app's own Bitbucket
// consumer, resolved from provider.CredentialStore.
func (p *Provider) endpointFor(ctx context.Context, appID uuid.UUID) (oauth.Endpoint, error) {
	cred, err := p.creds.Get(ctx, appID, db.TrackerBitbucket)
	if errors.Is(err, provider.ErrNoCredentials) {
		return oauth.Endpoint{}, apperr.NotFound("this app has no Bitbucket OAuth consumer yet; register one with PUT /apps/{appID}/providers/bitbucket/credentials")
	}
	if err != nil {
		return oauth.Endpoint{}, err
	}
	return oauth.Endpoint{
		Tracker:      "bitbucket",
		AuthURL:      p.opts.WebURL + "site/oauth2/authorize",
		TokenURL:     p.opts.WebURL + "site/oauth2/access_token",
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
		RedirectURL:  p.opts.RedirectURL,
		// Bitbucket takes the scopes from the consumer's permissions and
		// expects the client credentials with HTTP Basic.
		BasicAuth: true,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// InstallURL is Bitbucket's OAuth authorize page, for the app's own
// Bitbucket consumer.
func (p *Provider) InstallURL(ctx context.Context, appID uuid.UUID, state string) (string, error) {
	endpoint, err := p.endpointFor(ctx, appID)
	if err != nil {
		return "", err
	}
	return endpoint.AuthCodeURL(state), nil
}

// CompleteInstall exchanges the code (redeemable only with the app's own
// consumer secret, so it proves the grant) and reads the authorising user.
// The token becomes the connection's secret.
func (p *Provider) CompleteInstall(ctx context.Context, appID uuid.UUID, q url.Values) (provider.Account, error) {
	if e := q.Get("error"); e != "" {
		msg := e
		if d := q.Get("error_description"); d != "" {
			msg = d
		}
		return provider.Account{}, apperr.Invalid("bitbucket authorization was not granted: " + msg)
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
		UUID     string `json:"uuid"`
		Username string `json:"username"`
		Nickname string `json:"nickname"`
	}
	if _, err := p.api.Do(ctx, oauth.StaticToken(tok.AccessToken), http.MethodGet, "user", nil, &u); err != nil {
		return provider.Account{}, fmt.Errorf("bitbucket: read the authorizing user: %w", err)
	}
	if u.UUID == "" {
		return provider.Account{}, errors.New("bitbucket: the user endpoint returned no uuid")
	}
	login := u.Username
	if login == "" {
		login = u.Nickname
	}
	return provider.Account{ExternalID: u.UUID, Login: login, Type: "User", Secret: tok.Encode()}, nil
}

// ListRepositories lists the repositories the user is a member of,
// following Bitbucket's "next" links.
func (p *Provider) ListRepositories(ctx context.Context, c issue.Connection) ([]provider.Repository, error) {
	auth := p.tokens.For(c.ID, c.AppID)
	out := []provider.Repository{}
	next := "repositories?" + url.Values{"role": {"member"}, "pagelen": {strconv.Itoa(p.opts.PageSize)}}.Encode()
	for next != "" {
		var page struct {
			Values []struct {
				FullName  string `json:"full_name"`
				IsPrivate bool   `json:"is_private"`
				Links     struct {
					HTML struct {
						Href string `json:"href"`
					} `json:"html"`
				} `json:"links"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if _, err := p.api.Do(ctx, auth, http.MethodGet, next, nil, &page); err != nil {
			return nil, fmt.Errorf("bitbucket: list repositories: %w", err)
		}
		for _, r := range page.Values {
			out = append(out, provider.Repository{FullName: r.FullName, Private: r.IsPrivate, URL: r.Links.HTML.Href})
		}
		next = page.Next
	}
	return out, nil
}

// HasRepository reports whether the user can see the repository.
func (p *Provider) HasRepository(ctx context.Context, c issue.Connection, repository string) (bool, error) {
	path, err := repoPath(repository)
	if err != nil {
		return false, err
	}
	_, err = p.api.Do(ctx, p.tokens.For(c.ID, c.AppID), http.MethodGet, path, nil, nil)
	if oauth.IsStatus(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("bitbucket: get repository %s: %w", repository, err)
	}
	return true, nil
}

// namePattern is one part of workspace/repo_slug.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// ValidateRepository accepts workspace/repo_slug.
func (p *Provider) ValidateRepository(repository string) error {
	_, err := repoPath(repository)
	return err
}

// Covers is true for any valid name: an OAuth token reaches every
// repository its user can see, which cannot be told without asking
// Bitbucket. With several Bitbucket connections on one app, the first one
// is used.
func (p *Provider) Covers(_ issue.Connection, repository string) bool {
	return p.ValidateRepository(repository) == nil
}

func (p *Provider) Opener(_ context.Context, c issue.Connection) (issue.Opener, error) {
	return &opener{p: p, auth: p.tokens.For(c.ID, c.AppID)}, nil
}

// CreateWebhook implements provider.WebhookManager: registers a webhook
// for issue:updated, signed with a secret (checked via X-Hub-Signature,
// FR-56).
func (p *Provider) CreateWebhook(ctx context.Context, c issue.Connection, repository string) (provider.Webhook, error) {
	path, err := repoPath(repository)
	if err != nil {
		return provider.Webhook{}, err
	}
	secret, err := provider.RandomSecret()
	if err != nil {
		return provider.Webhook{}, err
	}
	req := map[string]any{
		"description": "m-Seismo",
		"url":         p.opts.WebhookURL,
		"active":      true,
		"events":      []string{"issue:updated"},
		"secret":      secret,
	}
	var created struct {
		UUID string `json:"uuid"`
	}
	if _, err := p.api.Do(ctx, p.tokens.For(c.ID, c.AppID), http.MethodPost, path+"/hooks", req, &created); err != nil {
		return provider.Webhook{}, fmt.Errorf("bitbucket: create webhook on %s: %w", repository, err)
	}
	return provider.Webhook{ExternalID: created.UUID, Secret: secret}, nil
}

// DeleteWebhook implements provider.WebhookManager.
func (p *Provider) DeleteWebhook(ctx context.Context, c issue.Connection, repository string, hook provider.Webhook) error {
	path, err := repoPath(repository)
	if err != nil {
		return err
	}
	if _, err := p.api.Do(ctx, p.tokens.For(c.ID, c.AppID), http.MethodDelete, path+"/hooks/"+url.PathEscape(hook.ExternalID), nil, nil); err != nil {
		return fmt.Errorf("bitbucket: delete webhook %s on %s: %w", hook.ExternalID, repository, err)
	}
	return nil
}

// repoPath is the API path of workspace/repo_slug.
func repoPath(repository string) (string, error) {
	ws, slug, ok := strings.Cut(repository, "/")
	if !ok || !namePattern.MatchString(ws) || !namePattern.MatchString(slug) {
		return "", apperr.Invalid("repository must be workspace/repo_slug")
	}
	return "repositories/" + url.PathEscape(ws) + "/" + url.PathEscape(slug), nil
}

func withSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

var (
	_ provider.Provider       = (*Provider)(nil)
	_ provider.WebhookManager = (*Provider)(nil)
)
