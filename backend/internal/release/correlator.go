package release

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var commitHashRegex = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func isCommitHash(s string) bool {
	return commitHashRegex.MatchString(strings.TrimSpace(s))
}

// CorrelateRequest represents the input parameters for correlating a release.
type CorrelateRequest struct {
	Tracker    string `json:"tracker"`    // "github", "gitlab", "bitbucket"
	Repository string `json:"repository"` // e.g. "owner/repo" or "group/subgroup/project"
	Version    string `json:"version"`    // e.g. "1.4.0" or "v1.4.0"
	Build      string `json:"build"`      // e.g. "82" or commit hash
}

// CorrelatedRelease contains resolved links and git context for a release.
type CorrelatedRelease struct {
	Version     string `json:"version"`
	Build       string `json:"build,omitempty"`
	TagName     string `json:"tag_name,omitempty"`
	PreviousTag string `json:"previous_tag,omitempty"`
	ReleaseURL  string `json:"release_url,omitempty"`
	CompareURL  string `json:"compare_url,omitempty"`
	CommitURL   string `json:"commit_url,omitempty"`
}

// ReleaseClient queries a repository tracker for tags.
type ReleaseClient interface {
	ListTags(ctx context.Context, tracker, repository string) ([]string, error)
}

// Correlator resolves release context and URLs with singleflight deduplication and caching.
type Correlator struct {
	client ReleaseClient
	cache  *Cache
}

// CorrelatorOption configures a Correlator.
type CorrelatorOption func(*Correlator)

// WithReleaseClient sets an external tracker client for tag resolution.
func WithReleaseClient(client ReleaseClient) CorrelatorOption {
	return func(c *Correlator) {
		c.client = client
	}
}

// WithReleaseCache sets a custom cache instance.
func WithReleaseCache(cache *Cache) CorrelatorOption {
	return func(c *Correlator) {
		c.cache = cache
	}
}

// NewCorrelator creates a new Correlator.
func NewCorrelator(opts ...CorrelatorOption) *Correlator {
	c := &Correlator{
		cache: NewCache(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Correlate resolves release notes and comparison diff URLs for a report's release.
func (c *Correlator) Correlate(ctx context.Context, req CorrelateRequest) (*CorrelatedRelease, error) {
	req.Version = strings.TrimSpace(req.Version)
	req.Repository = strings.TrimSpace(req.Repository)
	if req.Version == "" || req.Repository == "" {
		return nil, nil
	}

	key := fmt.Sprintf("%s:%s:%s:%s",
		strings.ToLower(req.Tracker),
		strings.ToLower(req.Repository),
		req.Version,
		req.Build,
	)

	return c.cache.GetOrCompute(key, func() (*CorrelatedRelease, error) {
		return c.resolve(ctx, req)
	})
}

func (c *Correlator) resolve(ctx context.Context, req CorrelateRequest) (*CorrelatedRelease, error) {
	host, cleanRepo := normalizeRepository(req.Repository)
	if cleanRepo == "" {
		cleanRepo = req.Repository
	}

	cleanVer := strings.TrimPrefix(req.Version, "v")
	tagName := "v" + cleanVer
	if strings.HasPrefix(req.Version, "v") {
		tagName = req.Version
	}
	previousTag := ""

	if c.client != nil {
		tags, err := c.client.ListTags(ctx, req.Tracker, cleanRepo)
		if err == nil && len(tags) > 0 {
			if matched, prev, ok := matchTag(tags, req.Version, req.Build); ok {
				tagName = matched
				previousTag = prev
			}
		}
	}

	result := &CorrelatedRelease{
		Version:     req.Version,
		Build:       req.Build,
		TagName:     tagName,
		PreviousTag: previousTag,
	}

	populateURLs(result, req.Tracker, host, cleanRepo, tagName, previousTag, req.Build)
	return result, nil
}

func matchTag(tags []string, version, build string) (matched string, prev string, found bool) {
	cleanVer := strings.TrimPrefix(version, "v")
	candidates := []string{
		version,
		"v" + cleanVer,
		cleanVer,
	}
	if build != "" {
		candidates = append([]string{
			fmt.Sprintf("%s+%s", version, build),
			fmt.Sprintf("v%s+%s", cleanVer, build),
			fmt.Sprintf("%s-%s", version, build),
			fmt.Sprintf("v%s-%s", cleanVer, build),
		}, candidates...)
	}

	for i, t := range tags {
		for _, cand := range candidates {
			if strings.EqualFold(t, cand) {
				var previous string
				if i+1 < len(tags) {
					previous = tags[i+1]
				}
				return t, previous, true
			}
		}
	}
	return "", "", false
}

func normalizeRepository(repo string) (host, path string) {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "git@")
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimSuffix(repo, ".git")

	// If it was git@github.com:owner/repo
	repo = strings.Replace(repo, ":", "/", 1)

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 && strings.Contains(parts[0], ".") {
		return parts[0], strings.Trim(parts[1], "/")
	}

	return "", strings.Trim(repo, "/")
}

func populateURLs(res *CorrelatedRelease, tracker, host, path, tagName, previousTag, build string) {
	trackerType := strings.ToLower(tracker)

	switch {
	case trackerType == "gitlab" || strings.Contains(host, "gitlab"):
		if host == "" {
			host = "gitlab.com"
		}
		base := fmt.Sprintf("https://%s/%s", host, path)
		res.ReleaseURL = fmt.Sprintf("%s/-/releases/%s", base, tagName)
		if previousTag != "" {
			res.CompareURL = fmt.Sprintf("%s/-/compare/%s...%s", base, previousTag, tagName)
		}
		if isCommitHash(build) {
			res.CommitURL = fmt.Sprintf("%s/-/commit/%s", base, build)
		}

	case trackerType == "bitbucket" || strings.Contains(host, "bitbucket"):
		if host == "" {
			host = "bitbucket.org"
		}
		base := fmt.Sprintf("https://%s/%s", host, path)
		res.ReleaseURL = fmt.Sprintf("%s/src/%s", base, tagName)
		if previousTag != "" {
			res.CompareURL = fmt.Sprintf("%s/branches/compare/%s%%0D%s", base, tagName, previousTag)
		}
		if isCommitHash(build) {
			res.CommitURL = fmt.Sprintf("%s/commits/%s", base, build)
		}

	default: // GitHub
		if host == "" {
			host = "github.com"
		}
		base := fmt.Sprintf("https://%s/%s", host, path)
		res.ReleaseURL = fmt.Sprintf("%s/releases/tag/%s", base, tagName)
		if previousTag != "" {
			res.CompareURL = fmt.Sprintf("%s/compare/%s...%s", base, previousTag, tagName)
		}
		if isCommitHash(build) {
			res.CommitURL = fmt.Sprintf("%s/commit/%s", base, build)
		}
	}
}
