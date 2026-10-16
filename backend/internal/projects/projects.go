// Package projects manages user projects. A project groups apps and the git
// repositories their issues are filed in.
package projects

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/audit"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/opt"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/paging"
)

const entityType = "project"

// Settings overrides service-wide defaults for one project. A nil field
// means "use the default from the configuration".
type Settings struct {
	RateLimitPerMinute             *int   `json:"rate_limit_per_minute,omitempty"`
	RateLimitPerInstallationMinute *int   `json:"rate_limit_per_installation_per_minute,omitempty"`
	AttachmentMaxBytes             *int64 `json:"attachment_max_bytes,omitempty"`
}

type Project struct {
	ID        uuid.UUID `json:"project_id"`
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Settings  Settings  `json:"settings"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name     string   `json:"name"`
	Slug     string   `json:"slug"` // derived from Name when empty
	Settings Settings `json:"settings"`
}

type UpdateInput struct {
	Name     opt.Field[string]   `json:"name"`
	Slug     opt.Field[string]   `json:"slug"`
	Settings opt.Field[Settings] `json:"settings"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

const columns = `project_id, user_id, name, slug, settings, created_at, updated_at`

func scan(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Slug, &p.Settings, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in CreateInput) (Project, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
	}
	var v apperr.Validator
	validateName(&v, in.Name)
	validateSlug(&v, in.Slug)
	validateSettings(&v, in.Settings)
	if err := v.Err(); err != nil {
		return Project{}, err
	}

	var p Project
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		p, err = scan(tx.QueryRow(ctx,
			`INSERT INTO projects (user_id, name, slug, settings) VALUES ($1, $2, $3, $4) RETURNING `+columns,
			userID, in.Name, in.Slug, in.Settings))
		if err != nil {
			return mapErr(err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &userID, Action: "project.create", EntityType: entityType, EntityID: p.ID, Payload: p,
		})
	})
	return p, err
}

func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Project, error) {
	p, err := scan(s.pool.QueryRow(ctx,
		`SELECT `+columns+` FROM projects WHERE user_id = $1 AND project_id = $2`, userID, id))
	if err != nil {
		return Project{}, mapErr(err)
	}
	return p, nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, page paging.Page) (paging.Result[Project], error) {
	page = page.Normalize()
	afterTS, afterID := page.AfterArgs()
	rows, err := s.pool.Query(ctx, `
		SELECT `+columns+` FROM projects
		WHERE user_id = $1 AND ($2::timestamptz IS NULL OR (created_at, project_id) > ($2, $3))
		ORDER BY created_at, project_id
		LIMIT $4`, userID, afterTS, afterID, page.Limit+1)
	if err != nil {
		return paging.Result[Project]{}, fmt.Errorf("projects: list: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Project, error) { return scan(r) })
	if err != nil {
		return paging.Result[Project]{}, fmt.Errorf("projects: list: %w", err)
	}
	return paging.Trim(items, page.Limit, func(p Project) paging.Cursor {
		return paging.Cursor{CreatedAt: p.CreatedAt, ID: p.ID}
	}), nil
}

func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in UpdateInput) (Project, error) {
	var v apperr.Validator
	if in.Name.Set {
		in.Name.Value = strings.TrimSpace(in.Name.Value)
		v.Check(!in.Name.Null, "name", "must not be null")
		validateName(&v, in.Name.Value)
	}
	if in.Slug.Set {
		in.Slug.Value = strings.TrimSpace(in.Slug.Value)
		v.Check(!in.Slug.Null, "slug", "must not be null")
		validateSlug(&v, in.Slug.Value)
	}
	if in.Settings.Set {
		// null resets every override to the defaults.
		validateSettings(&v, in.Settings.Value)
	}
	if err := v.Err(); err != nil {
		return Project{}, err
	}

	changes := map[string]any{}
	in.Name.PutIfSet(changes, "name")
	in.Slug.PutIfSet(changes, "slug")
	in.Settings.PutIfSet(changes, "settings")

	var p Project
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		p, err = scan(tx.QueryRow(ctx, `
			UPDATE projects SET
				name     = CASE WHEN $3 THEN $4 ELSE name END,
				slug     = CASE WHEN $5 THEN $6 ELSE slug END,
				settings = CASE WHEN $7 THEN $8::jsonb ELSE settings END
			WHERE user_id = $1 AND project_id = $2
			RETURNING `+columns,
			userID, id, in.Name.Set, in.Name.Value, in.Slug.Set, in.Slug.Value, in.Settings.Set, in.Settings.Value))
		if err != nil {
			return mapErr(err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &userID, Action: "project.update", EntityType: entityType, EntityID: p.ID, Payload: changes,
		})
	})
	return p, err
}

// Delete removes the project and, through ON DELETE CASCADE, its apps and
// repository links.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM projects WHERE user_id = $1 AND project_id = $2`, userID, id)
		if err != nil {
			return fmt.Errorf("projects: delete: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound(entityType)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &userID, Action: "project.delete", EntityType: entityType, EntityID: id,
		})
	})
}

// Exists reports whether the project exists and belongs to the user. Other
// packages call it to scope nested resources.
func Exists(ctx context.Context, q db.Querier, userID, id uuid.UUID) error {
	var ok bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM projects WHERE user_id = $1 AND project_id = $2)`, userID, id).Scan(&ok)
	if err != nil {
		return fmt.Errorf("projects: exists: %w", err)
	}
	if !ok {
		return apperr.NotFound(entityType)
	}
	return nil
}

var (
	slugRe      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	nonSlugRuns = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slugify lower-cases s and joins its ASCII alphanumeric runs with dashes.
func Slugify(s string) string {
	slug := strings.Trim(nonSlugRuns.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(slug) > 100 {
		slug = strings.TrimRight(slug[:100], "-")
	}
	return slug
}

func validateName(v *apperr.Validator, name string) {
	n := utf8.RuneCountInString(name)
	v.Check(n >= 1 && n <= 255, "name", "must be between 1 and 255 characters")
}

func validateSlug(v *apperr.Validator, slug string) {
	v.Check(len(slug) <= 100 && slugRe.MatchString(slug), "slug",
		"must be lower-case letters, digits and single dashes, at most 100 characters")
}

func validateSettings(v *apperr.Validator, s Settings) {
	if s.RateLimitPerMinute != nil {
		v.Check(*s.RateLimitPerMinute > 0, "settings.rate_limit_per_minute", "must be positive")
	}
	if s.RateLimitPerInstallationMinute != nil {
		v.Check(*s.RateLimitPerInstallationMinute > 0, "settings.rate_limit_per_installation_per_minute", "must be positive")
	}
	if s.AttachmentMaxBytes != nil {
		v.Check(*s.AttachmentMaxBytes > 0, "settings.attachment_max_bytes", "must be positive")
	}
}

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound(entityType)
	}
	if code, constraint, ok := db.PgError(err); ok {
		switch {
		case code == db.CodeUniqueViolation && constraint == "projects_user_slug_key":
			return apperr.Conflict("a project with this slug already exists")
		case code == db.CodeForeignKeyViolation && constraint == "projects_user_id_fkey":
			return apperr.NotFound("user")
		}
	}
	return fmt.Errorf("projects: %w", err)
}
