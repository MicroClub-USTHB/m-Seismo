package github

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// milestoneCacheTTL bounds how stale a cached milestone list may be. A
// milestone created on GitHub is picked up within this time.
const milestoneCacheTTL = 5 * time.Minute

type milestoneEntry struct {
	byTitle   map[string]int
	fetchedAt time.Time
}

// opener implements issue.Opener for one installation. Writes (create,
// comment) go through the installation's pacer; every call goes through
// Provider.call for rate-limit handling.
//
// Ref.ID has the form "owner/name#number" so Comment and IsOpen need
// nothing but the ref.
type opener struct {
	p    *Provider
	inst *installation
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

	var created *gh.Issue
	err = o.p.call(ctx, o.inst.id, o.inst.writes, func() (err error) {
		created, _, err = o.inst.client.Issues.Create(ctx, owner, name, req)
		return err
	})
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
	err = o.p.call(ctx, o.inst.id, o.inst.writes, func() error {
		_, _, err := o.inst.client.Issues.CreateComment(ctx, owner, name, number, gh.IssueCommentRequest{Body: body})
		return err
	})
	if err != nil {
		return fmt.Errorf("github: comment on %s: %w", ref.ID, err)
	}
	return nil
}

func (o *opener) IsOpen(ctx context.Context, ref issue.Ref) (bool, error) {
	owner, name, number, err := parseRef(ref.ID)
	if err != nil {
		return false, err
	}
	var got *gh.Issue
	err = o.p.call(ctx, o.inst.id, nil, func() (err error) {
		got, _, err = o.inst.client.Issues.Get(ctx, owner, name, number)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("github: get %s: %w", ref.ID, err)
	}
	return got.GetState() == "open", nil
}

// milestoneNumber resolves an open milestone by title; 0 means not found.
// Open milestones are cached per repository for milestoneCacheTTL.
func (o *opener) milestoneNumber(ctx context.Context, owner, name, title string) (int, error) {
	key := strconv.FormatInt(o.inst.id, 10) + "/" + owner + "/" + name

	o.p.milestoneMu.Lock()
	entry, ok := o.p.milestones[key]
	o.p.milestoneMu.Unlock()
	if ok && time.Since(entry.fetchedAt) < milestoneCacheTTL {
		return entry.byTitle[title], nil
	}

	byTitle := map[string]int{}
	opts := &gh.MilestoneListOptions{State: "open", ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		var ms []*gh.Milestone
		var resp *gh.Response
		err := o.p.call(ctx, o.inst.id, nil, func() (err error) {
			ms, resp, err = o.inst.client.Issues.ListMilestones(ctx, owner, name, opts)
			return err
		})
		if err != nil {
			return 0, fmt.Errorf("github: list milestones of %s/%s: %w", owner, name, err)
		}
		for _, m := range ms {
			byTitle[m.GetTitle()] = m.GetNumber()
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	o.p.milestoneMu.Lock()
	o.p.milestones[key] = milestoneEntry{byTitle: byTitle, fetchedAt: time.Now()}
	o.p.milestoneMu.Unlock()
	return byTitle[title], nil
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
