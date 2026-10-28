// Package routing decides where each report's issue goes and who gets it.
//
// Each project has rules, tried in position order. A rule matches a report
// when every condition it sets (app, severity, category) holds; the first
// match sends the issue to the rule's repository with its labels, assignees
// and milestone. A report that matches no rule goes to the project's
// default repository, with nothing added.
package routing

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// Facts are what rules match a report on.
type Facts struct {
	AppID    uuid.UUID
	Severity string
	Category string
}

// FactsOf reads the facts of a report. A crash the SDK sent on its own
// has no category, since nobody filled the report sheet; it counts as a
// crash, so a rule for crashes catches it.
func FactsOf(appID uuid.UUID, r *ingest.Report) Facts {
	f := Facts{AppID: appID}
	if in := r.UserInput; in != nil {
		f.Severity = in.Severity
		f.Category = in.Category
	}
	if f.Category == "" && (r.Trigger == "crash" || r.Trigger == "non_fatal") {
		f.Category = "crash"
	}
	return f
}

// Route is where one project's issue for a report goes.
type Route struct {
	ProjectID           uuid.UUID
	ProjectRepositoryID uuid.UUID
	Repository          string
	Labels              []string
	Assignees           []string
	Milestone           string
	// RuleID is the rule that matched, nil for the default repository.
	RuleID *uuid.UUID
}

// Queries is the part of db.Querier the router uses.
type Queries interface {
	ListIssueTargetsForApp(ctx context.Context, appID uuid.UUID) ([]db.ListIssueTargetsForAppRow, error)
	ListRoutingRulesForApp(ctx context.Context, appID uuid.UUID) ([]db.ListRoutingRulesForAppRow, error)
}

// Router picks the routes of a report.
type Router struct {
	q Queries
}

func NewRouter(q Queries) *Router {
	return &Router{q: q}
}

// Routes returns one route per project the report's app is linked to: the
// first matching rule's, or else the project's default repository. A
// project with neither gets no route.
func (r *Router) Routes(ctx context.Context, f Facts) ([]Route, error) {
	defaults, err := r.q.ListIssueTargetsForApp(ctx, f.AppID)
	if err != nil {
		return nil, fmt.Errorf("routing: default repositories of app %s: %w", f.AppID, err)
	}
	rules, err := r.q.ListRoutingRulesForApp(ctx, f.AppID)
	if err != nil {
		return nil, fmt.Errorf("routing: rules of app %s: %w", f.AppID, err)
	}

	// Rules come grouped by project and in order, so the first match per
	// project wins.
	matched := map[uuid.UUID]Route{}
	var order []uuid.UUID
	for _, rule := range rules {
		if _, done := matched[rule.ProjectID]; done || !matches(rule, f) {
			continue
		}
		id := rule.ID
		matched[rule.ProjectID] = Route{
			ProjectID:           rule.ProjectID,
			ProjectRepositoryID: rule.ProjectRepositoryID,
			Repository:          rule.Repository,
			Labels:              rule.Labels,
			Assignees:           rule.Assignees,
			Milestone:           deref(rule.Milestone),
			RuleID:              &id,
		}
		order = append(order, rule.ProjectID)
	}

	routes := make([]Route, 0, len(defaults)+len(order))
	seen := map[uuid.UUID]bool{}
	for _, d := range defaults {
		seen[d.ProjectID] = true
		if route, ok := matched[d.ProjectID]; ok {
			routes = append(routes, route)
			continue
		}
		routes = append(routes, Route{ProjectID: d.ProjectID, ProjectRepositoryID: d.ProjectRepositoryID, Repository: d.Repository})
	}
	// A project without a default repository still gets the issues its
	// rules route.
	for _, project := range order {
		if !seen[project] {
			routes = append(routes, matched[project])
		}
	}
	return routes, nil
}

// matches reports whether every condition the rule sets holds for f.
func matches(rule db.ListRoutingRulesForAppRow, f Facts) bool {
	if rule.AppID != nil && *rule.AppID != f.AppID {
		return false
	}
	if rule.Severity != nil && *rule.Severity != f.Severity {
		return false
	}
	if rule.Category != nil && *rule.Category != f.Category {
		return false
	}
	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
