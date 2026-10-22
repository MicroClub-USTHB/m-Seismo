// Package github connects apps to GitHub through a GitHub App and opens
// issues as that App. It is the only package that talks to the GitHub API.
//
// Access model: for one of their apps, the owner installs the m-Seismo
// GitHub App on a GitHub user or organisation (issues: write, metadata:
// read). The installation becomes a connection of that app whose external
// ID is the installation ID; apps of the same owner may share an
// installation. API calls use short-lived installation tokens minted from
// the App's private key, so no long-lived user token is ever stored.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v93/github"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
)

// Module registers the GitHub provider when a GitHub App is configured.
var Module = fx.Module("github",
	fx.Provide(provider.AsProvider(FromConfig)),
)

// FromConfig builds the provider from configuration. It returns no
// provider, rather than an error, when github.app_id is unset so the rest
// of the server can run without GitHub credentials.
func FromConfig(cfg *config.Config) ([]provider.Provider, error) {
	c := cfg.GitHub
	if c.AppID == 0 {
		slog.Warn("github provider disabled: github.app_id is not set")
		return nil, nil
	}

	key := []byte(c.PrivateKey)
	if len(key) == 0 && c.PrivateKeyPath != "" {
		var err error
		if key, err = os.ReadFile(c.PrivateKeyPath); err != nil {
			return nil, fmt.Errorf("github: read private key: %w", err)
		}
	}
	// Env vars often carry the PEM with literal "\n" sequences.
	if !strings.Contains(string(key), "\n") {
		key = []byte(strings.ReplaceAll(string(key), `\n`, "\n"))
	}

	p, err := New(Options{
		AppID:            c.AppID,
		AppSlug:          c.AppSlug,
		ClientID:         c.ClientID,
		ClientSecret:     c.ClientSecret,
		PrivateKey:       key,
		APIURL:           c.APIURL,
		WebURL:           c.WebURL,
		MinWriteInterval: c.MinWriteInterval,
		MaxRateLimitWait: c.MaxRateLimitWait,
	})
	if err != nil {
		return nil, err
	}
	return []provider.Provider{p}, nil
}

type Options struct {
	AppID        int64
	AppSlug      string
	ClientID     string
	ClientSecret string
	PrivateKey   []byte
	// APIURL is the REST API base, e.g. https://api.github.com/.
	APIURL string
	// WebURL is the web base used for install and OAuth URLs, e.g.
	// https://github.com/.
	WebURL string
	// MinWriteInterval spaces write requests per installation (see
	// ratelimit.go). Zero disables pacing; FromConfig defaults it to
	// DefaultMinWriteInterval.
	MinWriteInterval time.Duration
	// MaxRateLimitWait is the longest a call waits out a rate limit before
	// returning *issue.RateLimitError. Zero means DefaultMaxRateLimitWait;
	// negative means never wait.
	MaxRateLimitWait time.Duration
}

// Provider implements provider.Provider for GitHub.
type Provider struct {
	opts      Options
	appsTr    *ghinstallation.AppsTransport
	httpc     *http.Client
	mu        sync.Mutex
	installed map[int64]*installation

	milestoneMu sync.Mutex
	milestones  map[string]milestoneEntry
}

// installation is the per-installation state: an API client authenticated
// as the installation, and the pacer its write requests share.
type installation struct {
	id     int64
	client *gh.Client
	writes *pacer
}

func New(o Options) (*Provider, error) {
	if o.AppSlug == "" || o.ClientID == "" || o.ClientSecret == "" {
		return nil, errors.New("github: app_slug, client_id and client_secret are required")
	}
	if o.APIURL == "" {
		o.APIURL = "https://api.github.com/"
	}
	if o.WebURL == "" {
		o.WebURL = "https://github.com/"
	}
	o.APIURL = withSlash(o.APIURL)
	o.WebURL = withSlash(o.WebURL)
	if o.MaxRateLimitWait == 0 {
		o.MaxRateLimitWait = DefaultMaxRateLimitWait
	}

	appsTr, err := ghinstallation.NewAppsTransport(http.DefaultTransport, o.AppID, o.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("github: load app private key: %w", err)
	}
	appsTr.BaseURL = strings.TrimRight(o.APIURL, "/")

	return &Provider{
		opts:       o,
		appsTr:     appsTr,
		httpc:      &http.Client{Timeout: 15 * time.Second},
		installed:  make(map[int64]*installation),
		milestones: make(map[string]milestoneEntry),
	}, nil
}

func (p *Provider) Tracker() db.Tracker { return db.TrackerGithub }

// InstallURL sends the owner to the App's install page. GitHub returns to
// the App's configured Setup URL with installation_id, setup_action, state
// and (with "Request user authorization during installation") code.
//
// appID is unused: GitHub, unlike GitLab and Bitbucket, still uses the one
// App configured for the whole deployment.
func (p *Provider) InstallURL(_ context.Context, _ uuid.UUID, state string) (string, error) {
	return p.opts.WebURL + "apps/" + url.PathEscape(p.opts.AppSlug) + "/installations/new?state=" + url.QueryEscape(state), nil
}

// CompleteInstall trusts installation_id only after checking, with the
// installing user's own token, that the user can access that installation.
// Without this check anyone could attach someone else's installation by
// editing the callback URL.
//
// appID is unused; see InstallURL.
func (p *Provider) CompleteInstall(ctx context.Context, _ uuid.UUID, q url.Values) (provider.Account, error) {
	if q.Get("setup_action") == "request" {
		return provider.Account{}, apperr.Invalid("installation requested; an organization owner must approve it first")
	}
	installationID, err := strconv.ParseInt(q.Get("installation_id"), 10, 64)
	if err != nil || installationID <= 0 {
		return provider.Account{}, apperr.Invalid("missing or invalid installation_id")
	}
	code := q.Get("code")
	if code == "" {
		return provider.Account{}, apperr.Invalid("missing authorization code: enable \"Request user authorization (OAuth) during installation\" on the GitHub App")
	}

	token, err := p.exchangeCode(ctx, code)
	if err != nil {
		return provider.Account{}, err
	}
	userClient, err := gh.NewClient(gh.WithHTTPClient(p.httpc), gh.WithURLs(&p.opts.APIURL, nil), gh.WithAuthToken(token))
	if err != nil {
		return provider.Account{}, fmt.Errorf("github: user client: %w", err)
	}

	opts := &gh.ListOptions{PerPage: 100}
	for {
		var insts []*gh.Installation
		var resp *gh.Response
		err := p.call(ctx, installationID, nil, func() (err error) {
			insts, resp, err = userClient.Apps.ListUserInstallations(ctx, opts)
			return err
		})
		if err != nil {
			return provider.Account{}, fmt.Errorf("github: list user installations: %w", err)
		}
		for _, inst := range insts {
			if inst.GetID() == installationID {
				return provider.Account{
					ExternalID: strconv.FormatInt(installationID, 10),
					Login:      inst.GetAccount().GetLogin(),
					Type:       inst.GetAccount().GetType(),
				}, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return provider.Account{}, apperr.Invalid("the authorizing GitHub user cannot access this installation")
}

// exchangeCode trades an OAuth code for a user-to-server token. The token
// is used once, to verify the installation, and never stored.
func (p *Provider) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"client_id":     {p.opts.ClientID},
		"client_secret": {p.opts.ClientSecret},
		"code":          {code},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.opts.WebURL+"login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: exchange code: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("github: decode token response (status %d): %w", resp.StatusCode, err)
	}
	if body.Error != "" || body.AccessToken == "" {
		return "", apperr.Invalid("github rejected the authorization code: " + body.Error)
	}
	return body.AccessToken, nil
}

// installation returns the cached state for an installation, creating its
// API client on first use. One client per installation matters: go-github
// tracks rate limits per client, and ghinstallation caches and refreshes
// the installation token inside it.
func (p *Provider) installation(externalID string) (*installation, error) {
	id, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("github: invalid installation id %q", externalID)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if inst, ok := p.installed[id]; ok {
		return inst, nil
	}
	tr := &quotaWatcher{base: ghinstallation.NewFromAppsTransport(p.appsTr, id), installation: id}
	c, err := gh.NewClient(gh.WithTransport(tr), gh.WithURLs(&p.opts.APIURL, nil), gh.WithTimeout(30*time.Second))
	if err != nil {
		return nil, fmt.Errorf("github: installation client: %w", err)
	}
	inst := &installation{id: id, client: c, writes: &pacer{interval: p.opts.MinWriteInterval}}
	p.installed[id] = inst
	return inst, nil
}

func (p *Provider) ListRepositories(ctx context.Context, c issue.Connection) ([]provider.Repository, error) {
	inst, err := p.installation(c.ExternalID)
	if err != nil {
		return nil, err
	}

	out := []provider.Repository{}
	opts := &gh.ListOptions{PerPage: 100}
	for {
		var page *gh.ListRepositories
		var resp *gh.Response
		err := p.call(ctx, inst.id, nil, func() (err error) {
			page, resp, err = inst.client.Apps.ListRepos(ctx, opts)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("github: list installation repositories: %w", err)
		}
		for _, r := range page.Repositories {
			out = append(out, provider.Repository{FullName: r.GetFullName(), Private: r.GetPrivate(), URL: r.GetHTMLURL()})
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func (p *Provider) HasRepository(ctx context.Context, c issue.Connection, repository string) (bool, error) {
	owner, name, err := splitRepository(repository)
	if err != nil {
		return false, err
	}
	inst, err := p.installation(c.ExternalID)
	if err != nil {
		return false, err
	}
	err = p.call(ctx, inst.id, nil, func() error {
		_, _, err := inst.client.Repositories.Get(ctx, owner, name)
		return err
	})
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("github: get repository %s: %w", repository, err)
	}
	return true, nil
}

func (p *Provider) Opener(_ context.Context, c issue.Connection) (issue.Opener, error) {
	inst, err := p.installation(c.ExternalID)
	if err != nil {
		return nil, err
	}
	return &opener{p: p, inst: inst}, nil
}

// ValidateRepository accepts owner/name.
func (p *Provider) ValidateRepository(repository string) error {
	_, _, err := splitRepository(repository)
	return err
}

// Covers is true when the repository is owned by the installation's
// account: a GitHub App installation only ever reaches repositories of the
// user or organisation it is installed on. Whether the installation was
// given that particular repository is checked by HasRepository.
func (p *Provider) Covers(c issue.Connection, repository string) bool {
	owner, _, err := splitRepository(repository)
	return err == nil && strings.EqualFold(owner, c.AccountLogin)
}

func splitRepository(repository string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", apperr.Invalid("repository must be in owner/name form")
	}
	return owner, name, nil
}

func isNotFound(err error) bool {
	var er *gh.ErrorResponse
	return errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == http.StatusNotFound
}

func withSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

var _ provider.Provider = (*Provider)(nil)
