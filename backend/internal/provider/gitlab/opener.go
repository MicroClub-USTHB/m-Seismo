package gitlab

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/oauth"
)

// opener implements issue.Opener for one GitLab connection. Ref.ID has the
// form "group/project#iid" so Comment and IsOpen need nothing but the ref.
type opener struct {
	p    *Provider
	auth oauth.Auth
}

func (o *opener) Open(ctx context.Context, in issue.Issue) (issue.Ref, error) {
	if err := o.p.ValidateRepository(in.Repository); err != nil {
		return issue.Ref{}, err
	}
	req := map[string]any{"title": in.Title}
	if in.Body != "" {
		req["description"] = in.Body
	}
	if len(in.Labels) > 0 {
		req["labels"] = strings.Join(in.Labels, ",")
	}
	if in.Milestone != "" {
		id, err := o.milestoneID(ctx, in.Repository, in.Milestone)
		if err != nil {
			return issue.Ref{}, err
		}
		if id == 0 {
			slog.WarnContext(ctx, "gitlab milestone not found; opening issue without it",
				"repository", in.Repository, "milestone", in.Milestone)
		} else {
			req["milestone_id"] = id
		}
	}
	if len(in.Assignees) > 0 {
		ids, err := o.userIDs(ctx, in.Assignees)
		if err != nil {
			return issue.Ref{}, err
		}
		if len(ids) > 0 {
			req["assignee_ids"] = ids
		}
	}

	var created struct {
		IID    int    `json:"iid"`
		WebURL string `json:"web_url"`
	}
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodPost, projectPath(in.Repository)+"/issues", req, &created); err != nil {
		return issue.Ref{}, fmt.Errorf("gitlab: create issue in %s: %w", in.Repository, err)
	}
	return issue.Ref{ID: in.Repository + "#" + strconv.Itoa(created.IID), URL: created.WebURL}, nil
}

func (o *opener) Comment(ctx context.Context, ref issue.Ref, body string) error {
	repo, iid, err := parseRef(ref)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("%s/issues/%d/notes", projectPath(repo), iid)
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodPost, path, map[string]string{"body": body}, nil); err != nil {
		return fmt.Errorf("gitlab: comment on %s: %w", ref.ID, err)
	}
	return nil
}

func (o *opener) IsOpen(ctx context.Context, ref issue.Ref) (bool, error) {
	repo, iid, err := parseRef(ref)
	if err != nil {
		return false, err
	}
	var got struct {
		State string `json:"state"`
	}
	path := fmt.Sprintf("%s/issues/%d", projectPath(repo), iid)
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodGet, path, nil, &got); err != nil {
		return false, fmt.Errorf("gitlab: get %s: %w", ref.ID, err)
	}
	return got.State == "opened", nil
}

// milestoneID finds an active milestone by exact title; 0 when there is
// none.
func (o *opener) milestoneID(ctx context.Context, repo, title string) (int, error) {
	var ms []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}
	q := url.Values{"title": {title}, "state": {"active"}}
	if _, err := o.p.api.Do(ctx, o.auth, http.MethodGet, projectPath(repo)+"/milestones?"+q.Encode(), nil, &ms); err != nil {
		return 0, fmt.Errorf("gitlab: milestones of %s: %w", repo, err)
	}
	for _, m := range ms {
		if m.Title == title {
			return m.ID, nil
		}
	}
	return 0, nil
}

// userIDs resolves usernames to GitLab user IDs, skipping unknown ones.
func (o *opener) userIDs(ctx context.Context, usernames []string) ([]int, error) {
	ids := []int{}
	for _, name := range usernames {
		var users []struct {
			ID int `json:"id"`
		}
		if _, err := o.p.api.Do(ctx, o.auth, http.MethodGet, "users?"+url.Values{"username": {name}}.Encode(), nil, &users); err != nil {
			return nil, fmt.Errorf("gitlab: look up user %s: %w", name, err)
		}
		if len(users) == 0 {
			slog.WarnContext(ctx, "gitlab assignee not found; skipping", "username", name)
			continue
		}
		ids = append(ids, users[0].ID)
	}
	return ids, nil
}

// parseRef splits "group/project#iid".
func parseRef(ref issue.Ref) (string, int, error) {
	i := strings.LastIndex(ref.ID, "#")
	if i <= 0 {
		return "", 0, apperr.Invalid("gitlab issue ref must look like group/project#iid")
	}
	iid, err := strconv.Atoi(ref.ID[i+1:])
	if err != nil || iid <= 0 {
		return "", 0, apperr.Invalid("gitlab issue ref must look like group/project#iid")
	}
	return ref.ID[:i], iid, nil
}
