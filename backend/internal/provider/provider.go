// Package provider links apps to issue trackers (GitHub now, GitLab and
// Bitbucket later) and hands out per-connection issue openers. Every
// connection belongs to one app: each app brings its own credentials.
//
// Each tracker lives in its own subpackage that implements Provider and
// registers it in the "providers" fx group. Nothing here knows about a
// specific vendor.
package provider

import (
	"context"
	"net/url"
	"slices"

	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// Account is the tracker-side account a connection grants access to.
type Account struct {
	// ExternalID identifies the connection on the tracker (for GitHub, the
	// App installation ID).
	ExternalID string
	Login      string
	// Type is the tracker's account type, e.g. "User" or "Organization".
	Type string
}

// Repository is a repository reachable through a connection.
type Repository struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	URL      string `json:"url"`
}

// Provider is one issue tracker integration.
type Provider interface {
	Tracker() db.Tracker

	// InstallURL is where the owner's browser is sent to grant access. The
	// tracker sends state back to the callback unchanged.
	InstallURL(state string) string
	// CompleteInstall verifies the callback query and returns the account
	// the owner granted access to. It must not trust identifiers in the
	// query without checking them with the tracker.
	CompleteInstall(ctx context.Context, query url.Values) (Account, error)

	ListRepositories(ctx context.Context, c issue.Connection) ([]Repository, error)
	// HasRepository reports whether the connection can reach repository.
	HasRepository(ctx context.Context, c issue.Connection, repository string) (bool, error)

	// ValidateRepository checks the tracker's repository name format (e.g.
	// owner/name on GitHub) without calling the tracker.
	ValidateRepository(repository string) error
	// Covers reports whether the connection can, in principle, reach
	// repository, without calling the tracker. The issue service uses it to
	// pick one of an app's connections for each target.
	Covers(c issue.Connection, repository string) bool

	// Opener returns an issue opener acting through the connection.
	Opener(ctx context.Context, c issue.Connection) (issue.Opener, error)
}

// AsProvider annotates a constructor returning []Provider so its results
// join the "providers" group. A slice lets a provider opt out (return nil)
// when it is not configured.
func AsProvider(f any) any {
	return fx.Annotate(f, fx.ResultTags(`group:"providers,flatten"`))
}

// Registry looks providers up by tracker.
type Registry struct {
	providers map[db.Tracker]Provider
}

type registryParams struct {
	fx.In
	Providers []Provider `group:"providers"`
}

func NewRegistry(p registryParams) *Registry {
	r := &Registry{providers: make(map[db.Tracker]Provider, len(p.Providers))}
	for _, prov := range p.Providers {
		r.providers[prov.Tracker()] = prov
	}
	return r
}

// Get returns the provider for tracker, or a not-found error when the
// tracker is unknown or not configured on this deployment.
func (r *Registry) Get(tracker db.Tracker) (Provider, error) {
	p, ok := r.providers[tracker]
	if !ok {
		return nil, apperr.NotFound("provider " + string(tracker) + " is not available")
	}
	return p, nil
}

// Trackers lists the configured trackers.
func (r *Registry) Trackers() []db.Tracker {
	out := make([]db.Tracker, 0, len(r.providers))
	for t := range r.providers {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// Opener implements issue.OpenerSource.
func (r *Registry) Opener(ctx context.Context, c issue.Connection) (issue.Opener, error) {
	p, err := r.Get(c.Tracker)
	if err != nil {
		return nil, err
	}
	return p.Opener(ctx, c)
}

// Covers implements issue.OpenerSource. A connection on a tracker that is
// not configured covers nothing.
func (r *Registry) Covers(c issue.Connection, repository string) bool {
	p, err := r.Get(c.Tracker)
	if err != nil {
		return false
	}
	return p.Covers(c, repository)
}

var _ issue.OpenerSource = (*Registry)(nil)
