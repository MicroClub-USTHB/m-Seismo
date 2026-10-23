package routing

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// Limits on what a rule adds to an issue. GitHub caps label names at 50
// characters.
const (
	maxLabels      = 20
	maxLabelLen    = 50
	maxAssignees   = 10
	maxAssigneeLen = 100
	maxMilestone   = 255
)

var (
	severities = []string{"low", "medium", "high", "critical"}
	categories = []string{"bug", "crash", "ui", "other"}
)

// Rule is one routing rule of a project. A nil condition matches any
// report.
type Rule struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	Position     int32      `json:"position"`
	AppID        *uuid.UUID `json:"app_id"`
	Severity     *string    `json:"severity"`
	Category     *string    `json:"category"`
	RepositoryID uuid.UUID  `json:"repository_id"`
	Labels       []string   `json:"labels"`
	Assignees    []string   `json:"assignees"`
	Milestone    *string    `json:"milestone"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func toRule(r db.RoutingRule) Rule {
	return Rule{
		ID:           r.ID,
		ProjectID:    r.ProjectID,
		Position:     r.Position,
		AppID:        r.AppID,
		Severity:     r.Severity,
		Category:     r.Category,
		RepositoryID: r.ProjectRepositoryID,
		Labels:       r.Labels,
		Assignees:    r.Assignees,
		Milestone:    r.Milestone,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

// RuleInput creates or replaces a rule.
type RuleInput struct {
	// Position orders the rules, lowest first. Optional: a new rule goes
	// after the last one, a replaced rule keeps its place.
	Position     *int       `json:"position"`
	AppID        *uuid.UUID `json:"app_id"`
	Severity     *string    `json:"severity"`
	Category     *string    `json:"category"`
	RepositoryID uuid.UUID  `json:"repository_id"`
	Labels       []string   `json:"labels"`
	Assignees    []string   `json:"assignees"`
	Milestone    *string    `json:"milestone"`
}

// Service manages the routing rules of an owner's projects.
type Service struct {
	q db.Querier
}

func NewService(q db.Querier) *Service {
	return &Service{q: q}
}

// List returns the project's rules in the order they are tried.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID) ([]Rule, error) {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoutingRules(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Rule, len(rows))
	for i, r := range rows {
		out[i] = toRule(r)
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, in RuleInput) (Rule, error) {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return Rule{}, err
	}
	v, err := s.validate(ctx, ownerID, projectID, in)
	if err != nil {
		return Rule{}, err
	}
	position := v.position
	if position == nil {
		rows, err := s.q.ListRoutingRules(ctx, projectID)
		if err != nil {
			return Rule{}, err
		}
		next := int32(0)
		if len(rows) > 0 {
			next = rows[len(rows)-1].Position + 1
		}
		position = &next
	}

	r, err := s.q.CreateRoutingRule(ctx, db.CreateRoutingRuleParams{
		ProjectID:           projectID,
		Position:            *position,
		AppID:               in.AppID,
		Severity:            v.severity,
		Category:            v.category,
		ProjectRepositoryID: in.RepositoryID,
		Labels:              v.labels,
		Assignees:           v.assignees,
		Milestone:           v.milestone,
	})
	if err != nil {
		return Rule{}, err
	}
	return toRule(r), nil
}

// Replace overwrites a rule with in.
func (s *Service) Replace(ctx context.Context, ownerID, projectID, ruleID uuid.UUID, in RuleInput) (Rule, error) {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return Rule{}, err
	}
	current, err := s.q.GetRoutingRule(ctx, db.GetRoutingRuleParams{ID: ruleID, ProjectID: projectID})
	if err != nil {
		return Rule{}, notFound(err, "routing rule not found")
	}
	v, err := s.validate(ctx, ownerID, projectID, in)
	if err != nil {
		return Rule{}, err
	}
	position := current.Position
	if v.position != nil {
		position = *v.position
	}

	r, err := s.q.UpdateRoutingRule(ctx, db.UpdateRoutingRuleParams{
		ID:                  ruleID,
		ProjectID:           projectID,
		Position:            position,
		AppID:               in.AppID,
		Severity:            v.severity,
		Category:            v.category,
		ProjectRepositoryID: in.RepositoryID,
		Labels:              v.labels,
		Assignees:           v.assignees,
		Milestone:           v.milestone,
	})
	if err != nil {
		return Rule{}, notFound(err, "routing rule not found")
	}
	return toRule(r), nil
}

func (s *Service) Delete(ctx context.Context, ownerID, projectID, ruleID uuid.UUID) error {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return err
	}
	n, err := s.q.DeleteRoutingRule(ctx, db.DeleteRoutingRuleParams{ID: ruleID, ProjectID: projectID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("routing rule not found")
	}
	return nil
}

func (s *Service) ownProject(ctx context.Context, ownerID, projectID uuid.UUID) error {
	_, err := s.q.GetProject(ctx, db.GetProjectParams{ID: projectID, OwnerID: ownerID})
	return notFound(err, "project not found")
}

// validated is a RuleInput checked and cleaned up.
type validated struct {
	position           *int32
	severity, category *string
	labels, assignees  []string
	milestone          *string
}

func (s *Service) validate(ctx context.Context, ownerID, projectID uuid.UUID, in RuleInput) (validated, error) {
	var v validated

	if in.Position != nil {
		if *in.Position < 0 || *in.Position > math.MaxInt32 {
			return v, apperr.Invalid("position must be 0 or more")
		}
		p := int32(*in.Position)
		v.position = &p
	}

	var err error
	if v.severity, err = oneOf("severity", in.Severity, severities); err != nil {
		return v, err
	}
	if v.category, err = oneOf("category", in.Category, categories); err != nil {
		return v, err
	}
	if v.labels, err = names("labels", in.Labels, maxLabels, maxLabelLen); err != nil {
		return v, err
	}
	if v.assignees, err = names("assignees", in.Assignees, maxAssignees, maxAssigneeLen); err != nil {
		return v, err
	}
	for _, a := range v.assignees {
		if strings.ContainsAny(a, " \t\n@") {
			return v, apperr.Invalid("assignees are tracker usernames, without @ or spaces")
		}
	}
	if in.Milestone != nil {
		m := strings.TrimSpace(*in.Milestone)
		if utf8.RuneCountInString(m) > maxMilestone {
			return v, apperr.Invalid("milestone must be at most 255 characters")
		}
		if m != "" {
			v.milestone = &m
		}
	}

	if in.RepositoryID == uuid.Nil {
		return v, apperr.Invalid("repository_id is required")
	}
	if _, err := s.q.GetProjectRepository(ctx, db.GetProjectRepositoryParams{ID: in.RepositoryID, ProjectID: projectID}); err != nil {
		return v, notFound(err, "repository not found in this project")
	}
	if in.AppID != nil {
		if _, err := s.q.GetApp(ctx, db.GetAppParams{ID: *in.AppID, OwnerID: ownerID}); err != nil {
			return v, notFound(err, "app not found")
		}
	}
	return v, nil
}

// oneOf checks an optional enum value; empty means no condition.
func oneOf(field string, v *string, allowed []string) (*string, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	if !slices.Contains(allowed, *v) {
		return nil, apperr.Invalid(field + " must be one of " + strings.Join(allowed, ", "))
	}
	return v, nil
}

// names trims, drops empty and duplicate entries, and checks the limits.
func names(field string, in []string, maxCount, maxLen int) ([]string, error) {
	out := []string{}
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n == "" || slices.Contains(out, n) {
			continue
		}
		if utf8.RuneCountInString(n) > maxLen {
			return nil, apperr.Invalid(field + " entries must be at most " + strconv.Itoa(maxLen) + " characters")
		}
		out = append(out, n)
	}
	if len(out) > maxCount {
		return nil, apperr.Invalid(field + " can have at most " + strconv.Itoa(maxCount) + " entries")
	}
	return out, nil
}

// notFound turns a missing row into a not-found error with msg.
func notFound(err error, msg string) error {
	if err == nil {
		return nil
	}
	if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
		return apperr.NotFound(msg)
	}
	return err
}
