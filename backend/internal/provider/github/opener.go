package github

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// opener implements issue.Opener with one installation's client.
//
// Ref.ID has the form "owner/name#number" so Comment and IsOpen need
// nothing but the ref.
type opener struct {
	c *gh.Client
}

func (o *opener) Open(ctx context.Context, in issue.Issue) (issue.Ref, error) {
	owner, name, err := splitRepository(in.Repository)
	if err != nil {
		return issue.Ref{}, err
	}

	req := gh.CreateIssueRequest{
		Title:     in.Title,
		Labels:    in.Labels,
		Assignees: in.Assignees,
	}
	if in.Body != "" {
		req.Body = &in.Body
	}
	if in.Milestone != "" {
		number, err := o.milestoneNumber(ctx, owner, name, in.Milestone)
		if err != nil {
			return issue.Ref{}, err
		}
		if number == 0 {
			slog.WarnContext(ctx, "github milestone not found; opening issue without it",
				"repository", in.Repository, "milestone", in.Milestone)
		} else {
			req.Milestone = &number
		}
	}

	created, _, err := o.c.Issues.Create(ctx, owner, name, req)
	if err != nil {
		return issue.Ref{}, fmt.Errorf("github: create issue in %s: %w", in.Repository, err)
	}
	return issue.Ref{
		ID:  formatRef(owner, name, created.GetNumber()),
		URL: created.GetHTMLURL(),
	}, nil
}

func (o *opener) Comment(ctx context.Context, ref issue.Ref, body string) error {
	owner, name, number, err := parseRef(ref.ID)
	if err != nil {
		return err
	}
	if _, _, err := o.c.Issues.CreateComment(ctx, owner, name, number, gh.IssueCommentRequest{Body: body}); err != nil {
		return fmt.Errorf("github: comment on %s: %w", ref.ID, err)
	}
	return nil
}

func (o *opener) IsOpen(ctx context.Context, ref issue.Ref) (bool, error) {
	owner, name, number, err := parseRef(ref.ID)
	if err != nil {
		return false, err
	}
	got, _, err := o.c.Issues.Get(ctx, owner, name, number)
	if err != nil {
		return false, fmt.Errorf("github: get %s: %w", ref.ID, err)
	}
	return got.GetState() == "open", nil
}

// milestoneNumber resolves an open milestone by title; 0 means not found.
func (o *opener) milestoneNumber(ctx context.Context, owner, name, title string) (int, error) {
	opts := &gh.MilestoneListOptions{State: "open", ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		ms, resp, err := o.c.Issues.ListMilestones(ctx, owner, name, opts)
		if err != nil {
			return 0, fmt.Errorf("github: list milestones of %s/%s: %w", owner, name, err)
		}
		for _, m := range ms {
			if m.GetTitle() == title {
				return m.GetNumber(), nil
			}
		}
		if resp.NextPage == 0 {
			return 0, nil
		}
		opts.Page = resp.NextPage
	}
}

func formatRef(owner, name string, number int) string {
	return owner + "/" + name + "#" + strconv.Itoa(number)
}

func parseRef(id string) (owner, name string, number int, err error) {
	repo, num, ok := strings.Cut(id, "#")
	if !ok {
		return "", "", 0, fmt.Errorf("github: invalid issue ref %q", id)
	}
	if owner, name, err = splitRepository(repo); err != nil {
		return "", "", 0, fmt.Errorf("github: invalid issue ref %q", id)
	}
	if number, err = strconv.Atoi(num); err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("github: invalid issue ref %q", id)
	}
	return owner, name, number, nil
}
