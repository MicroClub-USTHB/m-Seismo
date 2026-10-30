// Package issuetemplate lets a project store its own issue layouts
// (FR-50): a routing rule can pick one, and without one the worker's
// default layout is used.
package issuetemplate

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

const (
	maxNameLen     = 200
	maxTemplateLen = 20000
)

type Template struct {
	ID            uuid.UUID `json:"id"`
	ProjectID     uuid.UUID `json:"project_id"`
	Name          string    `json:"name"`
	TitleTemplate string    `json:"title_template"`
	BodyTemplate  string    `json:"body_template"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toTemplate(t db.IssueTemplate) Template {
	return Template{
		ID: t.ID, ProjectID: t.ProjectID, Name: t.Name,
		TitleTemplate: t.TitleTemplate, BodyTemplate: t.BodyTemplate,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// Input creates or replaces a template.
type Input struct {
	Name          string `json:"name"`
	TitleTemplate string `json:"title_template"`
	BodyTemplate  string `json:"body_template"`
}

type Service struct {
	q db.Querier
}

func NewService(q db.Querier) *Service {
	return &Service{q: q}
}

func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID) ([]Template, error) {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListIssueTemplates(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Template, len(rows))
	for i, r := range rows {
		out[i] = toTemplate(r)
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, in Input) (Template, error) {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return Template{}, err
	}
	v, err := validate(in)
	if err != nil {
		return Template{}, err
	}
	t, err := s.q.CreateIssueTemplate(ctx, db.CreateIssueTemplateParams{
		ProjectID: projectID, Name: v.name, TitleTemplate: v.title, BodyTemplate: v.body,
	})
	if err != nil {
		return Template{}, err
	}
	return toTemplate(t), nil
}

func (s *Service) Replace(ctx context.Context, ownerID, projectID, templateID uuid.UUID, in Input) (Template, error) {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return Template{}, err
	}
	v, err := validate(in)
	if err != nil {
		return Template{}, err
	}
	t, err := s.q.UpdateIssueTemplate(ctx, db.UpdateIssueTemplateParams{
		ID: templateID, ProjectID: projectID, Name: v.name, TitleTemplate: v.title, BodyTemplate: v.body,
	})
	if err != nil {
		return Template{}, notFound(err, "issue template not found")
	}
	return toTemplate(t), nil
}

func (s *Service) Delete(ctx context.Context, ownerID, projectID, templateID uuid.UUID) error {
	if err := s.ownProject(ctx, ownerID, projectID); err != nil {
		return err
	}
	n, err := s.q.DeleteIssueTemplate(ctx, db.DeleteIssueTemplateParams{ID: templateID, ProjectID: projectID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("issue template not found")
	}
	return nil
}

func (s *Service) ownProject(ctx context.Context, ownerID, projectID uuid.UUID) error {
	_, err := s.q.GetProject(ctx, db.GetProjectParams{ID: projectID, OwnerID: ownerID})
	return notFound(err, "project not found")
}

type validated struct {
	name, title, body string
}

func validate(in Input) (validated, error) {
	var v validated
	var err error
	if v.name, err = validateName(in.Name); err != nil {
		return v, err
	}
	if v.title, err = validateTemplate("title", in.TitleTemplate); err != nil {
		return v, err
	}
	if v.body, err = validateTemplate("body", in.BodyTemplate); err != nil {
		return v, err
	}
	return v, nil
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return "", apperr.Invalid("name is required and must be at most 200 characters")
	}
	return name, nil
}

// validateTemplate requires tpl to be non-empty, within the length limit,
// and a template that both parses and only references fields TemplateData
// has.
func validateTemplate(field, raw string) (string, error) {
	tpl := strings.TrimSpace(raw)
	if tpl == "" {
		return "", apperr.Invalid(field + "_template is required")
	}
	if utf8.RuneCountInString(tpl) > maxTemplateLen {
		return "", apperr.Invalid(field + "_template must be at most 20000 characters")
	}
	if err := check(tpl); err != nil {
		return "", apperr.Invalid(field + "_template: " + err.Error())
	}
	return tpl, nil
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
