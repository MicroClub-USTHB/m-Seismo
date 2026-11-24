package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

const (
	defaultGitHubAPI    = "https://api.github.com"
	defaultGitLabAPI    = "https://gitlab.com/api/v4"
	defaultBitbucketAPI = "https://api.bitbucket.org/2.0"
	maxTagResponseBytes = 1 << 20
)

// HTTPClient queries tracker REST APIs for recent repository tags.
type HTTPClient struct {
	http         *http.Client
	githubAPI    string
	gitlabAPI    string
	bitbucketAPI string
}

// NewHTTPClient creates a ReleaseClient backed by the configured tracker API URLs.
func NewHTTPClient(cfg *config.Config) *HTTPClient {
	gh, gl, bb := defaultGitHubAPI, defaultGitLabAPI, defaultBitbucketAPI
	if cfg != nil {
		if u := strings.TrimRight(strings.TrimSpace(cfg.GitHub.APIURL), "/"); u != "" {
			gh = u
		}
		if u := strings.TrimRight(strings.TrimSpace(cfg.GitLab.APIURL), "/"); u != "" {
			gl = u
		}
		if u := strings.TrimRight(strings.TrimSpace(cfg.Bitbucket.APIURL), "/"); u != "" {
			bb = u
		}
	}
	return &HTTPClient{
		http:         &http.Client{Timeout: 3 * time.Second},
		githubAPI:    gh,
		gitlabAPI:    gl,
		bitbucketAPI: bb,
	}
}

// ListTags returns recent tag names for repository on tracker, newest first.
func (c *HTTPClient) ListTags(ctx context.Context, tracker, repository string) ([]string, error) {
	endpoint, err := c.tagsURL(tracker, repository)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req) //nolint:gosec // API base URLs come from operator config; repository path segments are escaped
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("release: %s tags lookup returned %d", tracker, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTagResponseBytes))
	if err != nil {
		return nil, err
	}

	return parseTagsResponse(tracker, body)
}

func (c *HTTPClient) tagsURL(tracker, repository string) (string, error) {
	repo := strings.Trim(strings.TrimSpace(repository), "/")
	if repo == "" {
		return "", fmt.Errorf("release: empty repository")
	}

	switch strings.ToLower(tracker) {
	case "gitlab":
		return fmt.Sprintf("%s/projects/%s/repository/tags?per_page=30", c.gitlabAPI, url.PathEscape(repo)), nil
	case "bitbucket":
		owner, name, ok := splitRepoTwoParts(repo)
		if !ok {
			return "", fmt.Errorf("release: invalid bitbucket repository %q", repository)
		}
		return fmt.Sprintf("%s/repositories/%s/%s/refs/tags?sort=-target.date&pagelen=30",
			c.bitbucketAPI, url.PathEscape(owner), url.PathEscape(name)), nil
	default:
		owner, name, ok := splitRepoTwoParts(repo)
		if !ok {
			return "", fmt.Errorf("release: invalid github repository %q", repository)
		}
		return fmt.Sprintf("%s/repos/%s/%s/tags?per_page=30",
			c.githubAPI, url.PathEscape(owner), url.PathEscape(name)), nil
	}
}

func splitRepoTwoParts(repo string) (string, string, bool) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseTagsResponse(tracker string, body []byte) ([]string, error) {
	if strings.EqualFold(tracker, "bitbucket") {
		var payload struct {
			Values []struct {
				Name string `json:"name"`
			} `json:"values"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("release: decode bitbucket tags: %w", err)
		}
		tags := make([]string, 0, len(payload.Values))
		for _, v := range payload.Values {
			if v.Name != "" {
				tags = append(tags, v.Name)
			}
		}
		return tags, nil
	}

	var items []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("release: decode %s tags: %w", tracker, err)
	}
	tags := make([]string, 0, len(items))
	for _, it := range items {
		if it.Name != "" {
			tags = append(tags, it.Name)
		}
	}
	return tags, nil
}
