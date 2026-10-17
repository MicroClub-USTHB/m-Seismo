// Package project manages an owner's projects and the tracker repositories
// each project sends issues to.
package project

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
)

const (
	maxNameLen = 200
	maxSlugLen = 64
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	nonSlugRun  = regexp.MustCompile(`[^a-z0-9]+`)
)

type Project struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toProject(p db.Project) Project {
	return Project{ID: p.ID, Name: p.Name, Slug: p.Slug, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// Repository is a tracker repository linked to a project.
type Repository struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	ConnectionID uuid.UUID  `json:"connection_id"`
	Tracker      db.Tracker `json:"tracker"`
	AccountLogin string     `json:"account_login"`
	Repository   string     `json:"repository"`
	IsDefault    bool       `json:"is_default"`
	CreatedAt    time.Time  `json:"created_at"`
}

type Service struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	providers *provider.Service
}

func NewService(pool *pgxpool.Pool, q *db.Queries, providers *provider.Service) *Service {
	return &Service{pool: pool, q: q, providers: providers}
}

type CreateInput struct {
	Name string `json:"name"`
	// Slug is derived from Name when empty.
	Slug string `json:"slug"`
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, in CreateInput) (Project, error) {
	name, err := validateName(in.Name)
	if err != nil {
		return Project{}, err
	}
	slug := in.Slug
	if slug == "" {
		slug = slugify(name)
	}
	if err := validateSlug(slug); err != nil {
		return Project{}, err
	}

	p, err := s.q.CreateProject(ctx, db.CreateProjectParams{OwnerID: ownerID, Name: name, Slug: slug})
	if err != nil {
		return Project{}, slugConflict(err)
	}
	return toProject(p), nil
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]Project, error) {
	rows, err := s.q.ListProjects(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]Project, len(rows))
	for i, p := range rows {
		out[i] = toProject(p)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, ownerID, id uuid.UUID) (Project, error) {
	p, err := s.get(ctx, ownerID, id)
	if err != nil {
		return Project{}, err
	}
	return toProject(p), nil
}

type UpdateInput struct {
	Name *string `json:"name"`
	Slug *string `json:"slug"`
}

func (s *Service) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateInput) (Project, error) {
	p, err := s.get(ctx, ownerID, id)
	if err != nil {
		return Project{}, err
	}
	params := db.UpdateProjectParams{ID: p.ID, OwnerID: ownerID, Name: p.Name, Slug: p.Slug}
	if in.Name != nil {
		if params.Name, err = validateName(*in.Name); err != nil {
			return Project{}, err
		}
	}
	if in.Slug != nil {
		if err := validateSlug(*in.Slug); err != nil {
			return Project{}, err
		}
		params.Slug = *in.Slug
	}

	updated, err := s.q.UpdateProject(ctx, params)
	if err != nil {
		return Project{}, slugConflict(err)
	}
	return toProject(updated), nil
}

func (s *Service) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.q.DeleteProject(ctx, db.DeleteProjectParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("project not found")
	}
	return nil
}

func (s *Service) ListRepositories(ctx context.Context, ownerID, projectID uuid.UUID) ([]Repository, error) {
	if _, err := s.get(ctx, ownerID, projectID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListProjectRepositories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Repository, len(rows))
	for i, r := range rows {
		out[i] = Repository{
			ID:           r.ID,
			ProjectID:    r.ProjectID,
			ConnectionID: r.ProviderConnectionID,
			Tracker:      r.Tracker,
			AccountLogin: r.AccountLogin,
			Repository:   r.Repository,
			IsDefault:    r.IsDefault,
			CreatedAt:    r.CreatedAt,
		}
	}
	return out, nil
}

type LinkRepositoryInput struct {
	ConnectionID uuid.UUID `json:"connection_id"`
	// Repository is the tracker's full name, e.g. "octo-org/app" on GitHub.
	Repository string `json:"repository"`
	// IsDefault makes this the project's default repository (FR-59). The
	// first repository linked to a project is always the default.
	IsDefault bool `json:"is_default"`
}

// LinkRepository links a repository, reached through one of the owner's
// connections, to the project. The tracker is asked whether the connection
// can actually reach it.
func (s *Service) LinkRepository(ctx context.Context, ownerID, projectID uuid.UUID, in LinkRepositoryInput) (Repository, error) {
	if _, err := s.get(ctx, ownerID, projectID); err != nil {
		return Repository{}, err
	}
	repository := strings.TrimSpace(in.Repository)
	if repository == "" {
		return Repository{}, apperr.Invalid("repository is required")
	}
	conn, err := s.providers.VerifyRepository(ctx, ownerID, in.ConnectionID, repository)
	if err != nil {
		return Repository{}, err
	}

	var linked db.ProjectRepository
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		existing, err := q.ListProjectRepositories(ctx, projectID)
		if err != nil {
			return err
		}
		isDefault := in.IsDefault || len(existing) == 0
		if isDefault {
			if err := q.ClearDefaultProjectRepository(ctx, projectID); err != nil {
				return err
			}
		}

		linked, err = q.LinkProjectRepository(ctx, db.LinkProjectRepositoryParams{
			ProjectID:            projectID,
			ProviderConnectionID: conn.ID,
			Repository:           repository,
			IsDefault:            isDefault,
		})
		return err
	})
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindConflict {
			return Repository{}, apperr.Conflict("repository is already linked to this project")
		}
		return Repository{}, err
	}

	return Repository{
		ID:           linked.ID,
		ProjectID:    linked.ProjectID,
		ConnectionID: linked.ProviderConnectionID,
		Tracker:      conn.Tracker,
		AccountLogin: conn.AccountLogin,
		Repository:   linked.Repository,
		IsDefault:    linked.IsDefault,
		CreatedAt:    linked.CreatedAt,
	}, nil
}

// SetDefaultRepository makes repositoryID the project's default.
func (s *Service) SetDefaultRepository(ctx context.Context, ownerID, projectID, repositoryID uuid.UUID) error {
	if _, err := s.get(ctx, ownerID, projectID); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ClearDefaultProjectRepository(ctx, projectID); err != nil {
			return err
		}
		n, err := q.SetDefaultProjectRepository(ctx, db.SetDefaultProjectRepositoryParams{ID: repositoryID, ProjectID: projectID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("repository not found")
		}
		return nil
	})
}

func (s *Service) UnlinkRepository(ctx context.Context, ownerID, projectID, repositoryID uuid.UUID) error {
	if _, err := s.get(ctx, ownerID, projectID); err != nil {
		return err
	}
	n, err := s.q.UnlinkProjectRepository(ctx, db.UnlinkProjectRepositoryParams{ID: repositoryID, ProjectID: projectID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("repository not found")
	}
	return nil
}

func (s *Service) get(ctx context.Context, ownerID, id uuid.UUID) (db.Project, error) {
	p, err := s.q.GetProject(ctx, db.GetProjectParams{ID: id, OwnerID: ownerID})
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
			return db.Project{}, apperr.NotFound("project not found")
		}
		return db.Project{}, err
	}
	return p, nil
}

func slugConflict(err error) error {
	if kind, _ := apperr.Classify(err); kind == apperr.KindConflict {
		return apperr.Conflict("a project with this slug already exists")
	}
	return err
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > maxNameLen {
		return "", apperr.Invalid("name is required and must be at most 200 characters")
	}
	return name, nil
}

func validateSlug(slug string) error {
	if len(slug) > maxSlugLen || !slugPattern.MatchString(slug) {
		return apperr.Invalid("slug must be lowercase letters, digits and single dashes, at most 64 characters")
	}
	return nil
}

func slugify(name string) string {
	s := strings.Trim(nonSlugRun.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > maxSlugLen {
		s = strings.TrimRight(s[:maxSlugLen], "-")
	}
	return s
}
