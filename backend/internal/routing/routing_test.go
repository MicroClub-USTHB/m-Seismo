package routing

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

type fakeQueries struct {
	defaults []db.ListIssueTargetsForAppRow
	rules    []db.ListRoutingRulesForAppRow
}

func (f *fakeQueries) ListIssueTargetsForApp(context.Context, uuid.UUID) ([]db.ListIssueTargetsForAppRow, error) {
	return f.defaults, nil
}

func (f *fakeQueries) ListRoutingRulesForApp(context.Context, uuid.UUID) ([]db.ListRoutingRulesForAppRow, error) {
	return f.rules, nil
}

func ptr[T any](v T) *T { return &v }

func TestFactsOf(t *testing.T) {
	app := uuid.New()
	tests := []struct {
		name string
		r    ingest.Report
		want Facts
	}{
		{"report sheet", ingest.Report{Trigger: "shake", UserInput: &ingest.UserInput{Severity: "high", Category: "ui"}}, Facts{app, "high", "ui"}},
		{"automatic crash", ingest.Report{Trigger: "crash"}, Facts{app, "", "crash"}},
		{"non-fatal error", ingest.Report{Trigger: "non_fatal"}, Facts{app, "", "crash"}},
		{"crash the reporter filed as a bug", ingest.Report{Trigger: "crash", UserInput: &ingest.UserInput{Category: "bug"}}, Facts{app, "", "bug"}},
		{"shake without a category", ingest.Report{Trigger: "shake"}, Facts{app, "", ""}},
	}
	for _, tt := range tests {
		if got := FactsOf(app, &tt.r); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestRoutes(t *testing.T) {
	app, other := uuid.New(), uuid.New()
	p1, p2, p3 := uuid.New(), uuid.New(), uuid.New()
	def1, def2, triage, mobile := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	critical := db.ListRoutingRulesForAppRow{ID: uuid.New(), ProjectID: p1, Severity: ptr("critical"), ProjectRepositoryID: triage, Repository: "o/triage", Labels: []string{"urgent"}}
	crashes := db.ListRoutingRulesForAppRow{ID: uuid.New(), ProjectID: p1, Category: ptr("crash"), ProjectRepositoryID: mobile, Repository: "o/mobile", Labels: []string{"crash"}, Assignees: []string{"octocat"}, Milestone: ptr("v2")}
	otherApp := db.ListRoutingRulesForAppRow{ID: uuid.New(), ProjectID: p2, AppID: &other, ProjectRepositoryID: triage, Repository: "o/triage"}
	// p3 has no default repository, only a rule.
	p3Crashes := db.ListRoutingRulesForAppRow{ID: uuid.New(), ProjectID: p3, AppID: &app, Category: ptr("crash"), ProjectRepositoryID: mobile, Repository: "o/mobile"}

	router := NewRouter(&fakeQueries{
		defaults: []db.ListIssueTargetsForAppRow{
			{ProjectID: p1, ProjectRepositoryID: def1, Repository: "o/one"},
			{ProjectID: p2, ProjectRepositoryID: def2, Repository: "o/two"},
		},
		rules: []db.ListRoutingRulesForAppRow{critical, crashes, otherApp, p3Crashes},
	})

	tests := []struct {
		name  string
		facts Facts
		want  map[uuid.UUID]uuid.UUID // project -> repository
		rule  map[uuid.UUID]uuid.UUID // project -> matched rule
	}{
		{
			name:  "no rule matches",
			facts: Facts{AppID: app, Severity: "low", Category: "ui"},
			want:  map[uuid.UUID]uuid.UUID{p1: def1, p2: def2},
		},
		{
			name:  "rule by category, and a project with only rules",
			facts: Facts{AppID: app, Category: "crash"},
			want:  map[uuid.UUID]uuid.UUID{p1: mobile, p2: def2, p3: mobile},
			rule:  map[uuid.UUID]uuid.UUID{p1: crashes.ID, p3: p3Crashes.ID},
		},
		{
			name:  "first matching rule wins",
			facts: Facts{AppID: app, Severity: "critical", Category: "crash"},
			want:  map[uuid.UUID]uuid.UUID{p1: triage, p2: def2, p3: mobile},
			rule:  map[uuid.UUID]uuid.UUID{p1: critical.ID, p3: p3Crashes.ID},
		},
	}
	for _, tt := range tests {
		routes, err := router.Routes(context.Background(), tt.facts)
		if err != nil {
			t.Fatal(err)
		}
		if len(routes) != len(tt.want) {
			t.Errorf("%s: got %d routes, want %d", tt.name, len(routes), len(tt.want))
		}
		for _, r := range routes {
			if r.ProjectRepositoryID != tt.want[r.ProjectID] {
				t.Errorf("%s: project %s goes to %s, want %s", tt.name, r.ProjectID, r.ProjectRepositoryID, tt.want[r.ProjectID])
			}
			wantRule, matched := tt.rule[r.ProjectID]
			switch {
			case matched && (r.RuleID == nil || *r.RuleID != wantRule):
				t.Errorf("%s: project %s matched rule %v, want %s", tt.name, r.ProjectID, r.RuleID, wantRule)
			case !matched && (r.RuleID != nil || len(r.Labels) > 0):
				t.Errorf("%s: project %s got rule %v and labels %v, want the plain default", tt.name, r.ProjectID, r.RuleID, r.Labels)
			}
		}
	}

	routes, _ := router.Routes(context.Background(), Facts{AppID: app, Category: "crash"})
	if r := routes[0]; r.Milestone != "v2" || r.Assignees[0] != "octocat" || r.Labels[0] != "crash" {
		t.Errorf("route lost the rule's labels, assignees or milestone: %+v", r)
	}
}
