package bitbucket

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/oauth"
)

// openStates are the Bitbucket issue states that count as open; resolved,
// invalid, duplicate, wontfix and closed do not.
var openStates = map[string]bool{"new": true, "open": true, "on hold": true}

// opener implements issue.Opener for one Bitbucket connection. Ref.ID has
// the form "workspace/repo_slug#id".
type opener struct {
	p    *Provider
	auth oauth.Auth
}

func (o *opener) Open(ctx context.Context, in issue.Issue) (issue.Ref, error) {
	path, err := repoPath(in.Repository)
	if err != nil {
		return issue.Ref{}, err
	}

	content := in.Body
	if len(in.Labels) > 0 {
		// Bitbucket issues have no labels; keep them visible in the body.
		if content != "" {
			content += "\n\n"
		}
		content += "Labels: " + strings.Join(in.Labels, ", ")
	}
	req := map[string]any{
		"title":    in.Title,
		"kind":     "bug",
		"priority": "major",
	}
	if content != "" {
		req["content"] = map[string]string{"raw": content, "markup": "markdown"}
	}
	if in.Milestone != "" {
		ok, err := o.hasMilestone(ctx, path, in.Milestone)
		if err != nil {
			return issue.Ref{}, fmt.Errorf("bitbucket: milestones of %s: %w", in.Repository, err)
		}
		if ok {
			req["milestone"] = map[string]string{"name": in.Milestone}
		} else {
			slog.WarnContext(ctx, "bitbucket milestone not found; opening issue without it",
				"repository", in.Repository, "milestone", in.Milestone)
		}
	}
	if len(in.Assignees) > 0 {
		slog.WarnContext(ctx, "bitbucket assignees need Atlassian account IDs; skipping them",
			"repository", in.Repository, "assignees", in.Assignees)
	}

	var created struct {
		ID    int `json:"id"`
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodPost, path+"/issues", req, &created); err != nil {
		return issue.Ref{}, fmt.Errorf("bitbucket: create issue in %s: %w", in.Repository, err)
	}
	return issue.Ref{ID: in.Repository + "#" + strconv.Itoa(created.ID), URL: created.Links.HTML.Href}, nil
}

func (o *opener) Comment(ctx context.Context, ref issue.Ref, body string) error {
	path, id, err := parseRef(ref)
	if err != nil {
		return err
	}
	req := map[string]any{"content": map[string]string{"raw": body}}
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", path, id), req, nil); err != nil {
		return fmt.Errorf("bitbucket: comment on %s: %w", ref.ID, err)
	}
	return nil
}

func (o *opener) IsOpen(ctx context.Context, ref issue.Ref) (bool, error) {
	path, id, err := parseRef(ref)
	if err != nil {
		return false, err
	}
	var got struct {
		State string `json:"state"`
	}
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodGet, fmt.Sprintf("%s/issues/%d", path, id), nil, &got); err != nil {
		return false, fmt.Errorf("bitbucket: get %s: %w", ref.ID, err)
	}
	return openStates[got.State], nil
}

// hasMilestone looks for a milestone by exact name in the first page (up
// to 100 milestones).
func (o *opener) hasMilestone(ctx context.Context, path, name string) (bool, error) {
	var page struct {
		Values []struct {
			Name string `json:"name"`
		} `json:"values"`
	}
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodGet, path+"/milestones?pagelen=100", nil, &page); err != nil {
		return false, err
	}
	for _, m := range page.Values {
		if m.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// parseRef splits "workspace/repo_slug#id" into the repository API path
// and the issue ID.
func parseRef(ref issue.Ref) (string, int, error) {
	repo, num, ok := strings.Cut(ref.ID, "#")
	id, err := strconv.Atoi(num)
	if !ok || err != nil || id <= 0 {
		return "", 0, apperr.Invalid("bitbucket issue ref must look like workspace/repo_slug#id")
	}
	path, err := repoPath(repo)
	if err != nil {
		return "", 0, err
	}
	return path, id, nil
}
