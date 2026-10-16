// Package apps manages the mobile apps of a project. Every app is reached
// through its owning user and project, so one user can never touch another
// user's apps by guessing ids.
package apps

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
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/projects"
)

const entityType = "app"

type Platform string

const (
	PlatformIOS         Platform = "ios"
	PlatformAndroid     Platform = "android"
	PlatformFlutter     Platform = "flutter"
	PlatformReactNative Platform = "react_native"
)

func (p Platform) Valid() bool {
	switch p {
	case PlatformIOS, PlatformAndroid, PlatformFlutter, PlatformReactNative:
		return true
	}
	return false
}

type App struct {
	ID          uuid.UUID `json:"app_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Name        string    `json:"name"`
	Platform    Platform  `json:"platform"`
	BundleID    string    `json:"bundle_id"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name        string   `json:"name"`
	Platform    Platform `json:"platform"`
	BundleID    string   `json:"bundle_id"`
	Environment string   `json:"environment"` // "production" when empty
}

type UpdateInput struct {
	Name        opt.Field[string]   `json:"name"`
	Platform    opt.Field[Platform] `json:"platform"`
	BundleID    opt.Field[string]   `json:"bundle_id"`
	Environment opt.Field[string]   `json:"environment"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

const columns = `a.app_id, a.project_id, a.name, a.platform, a.bundle_id, a.environment, a.created_at, a.updated_at`

func scan(row pgx.Row) (App, error) {
	var a App
	err := row.Scan(&a.ID, &a.ProjectID, &a.Name, &a.Platform, &a.BundleID, &a.Environment, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, in CreateInput) (App, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BundleID = strings.TrimSpace(in.BundleID)
	in.Environment = strings.TrimSpace(in.Environment)
	if in.Environment == "" {
		in.Environment = "production"
	}
	var v apperr.Validator
	validateName(&v, in.Name)
	validatePlatform(&v, in.Platform)
	validateBundleID(&v, in.BundleID)
	validateEnvironment(&v, in.Environment)
	if err := v.Err(); err != nil {
		return App{}, err
	}

	var a App
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := projects.Exists(ctx, tx, userID, projectID); err != nil {
			return err
		}
		var err error
		a, err = scan(tx.QueryRow(ctx, `
			INSERT INTO apps AS a (project_id, name, platform, bundle_id, environment)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+columns,
			projectID, in.Name, in.Platform, in.BundleID, in.Environment))
		if err != nil {
			return mapErr(err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &userID, Action: "app.create", EntityType: entityType, EntityID: a.ID, Payload: a,
		})
	})
	return a, err
}

func (s *Service) Get(ctx context.Context, userID, projectID, id uuid.UUID) (App, error) {
	a, err := scan(s.pool.QueryRow(ctx, `
		SELECT `+columns+` FROM apps a JOIN projects p USING (project_id)
		WHERE p.user_id = $1 AND a.project_id = $2 AND a.app_id = $3`, userID, projectID, id))
	if err != nil {
		return App{}, mapErr(err)
	}
	return a, nil
}

func (s *Service) List(ctx context.Context, userID, projectID uuid.UUID, page paging.Page) (paging.Result[App], error) {
	if err := projects.Exists(ctx, s.pool, userID, projectID); err != nil {
		return paging.Result[App]{}, err
	}
	page = page.Normalize()
	afterTS, afterID := page.AfterArgs()
	rows, err := s.pool.Query(ctx, `
		SELECT `+columns+` FROM apps a
		WHERE a.project_id = $1 AND ($2::timestamptz IS NULL OR (a.created_at, a.app_id) > ($2, $3))
		ORDER BY a.created_at, a.app_id
		LIMIT $4`, projectID, afterTS, afterID, page.Limit+1)
	if err != nil {
		return paging.Result[App]{}, fmt.Errorf("apps: list: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (App, error) { return scan(r) })
	if err != nil {
		return paging.Result[App]{}, fmt.Errorf("apps: list: %w", err)
	}
	return paging.Trim(items, page.Limit, func(a App) paging.Cursor {
		return paging.Cursor{CreatedAt: a.CreatedAt, ID: a.ID}
	}), nil
}

func (s *Service) Update(ctx context.Context, userID, projectID, id uuid.UUID, in UpdateInput) (App, error) {
	var v apperr.Validator
	if in.Name.Set {
		in.Name.Value = strings.TrimSpace(in.Name.Value)
		v.Check(!in.Name.Null, "name", "must not be null")
		validateName(&v, in.Name.Value)
	}
	if in.Platform.Set {
		v.Check(!in.Platform.Null, "platform", "must not be null")
		validatePlatform(&v, in.Platform.Value)
	}
	if in.BundleID.Set {
		in.BundleID.Value = strings.TrimSpace(in.BundleID.Value)
		v.Check(!in.BundleID.Null, "bundle_id", "must not be null")
		validateBundleID(&v, in.BundleID.Value)
	}
	if in.Environment.Set {
		in.Environment.Value = strings.TrimSpace(in.Environment.Value)
		v.Check(!in.Environment.Null, "environment", "must not be null")
		validateEnvironment(&v, in.Environment.Value)
	}
	if err := v.Err(); err != nil {
		return App{}, err
	}

	changes := map[string]any{}
	in.Name.PutIfSet(changes, "name")
	in.Platform.PutIfSet(changes, "platform")
	in.BundleID.PutIfSet(changes, "bundle_id")
	in.Environment.PutIfSet(changes, "environment")

	var a App
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		a, err = scan(tx.QueryRow(ctx, `
			UPDATE apps AS a SET
				name        = CASE WHEN $4 THEN $5 ELSE a.name END,
				platform    = CASE WHEN $6 THEN $7 ELSE a.platform END,
				bundle_id   = CASE WHEN $8 THEN $9 ELSE a.bundle_id END,
				environment = CASE WHEN $10 THEN $11 ELSE a.environment END
			FROM projects p
			WHERE p.project_id = a.project_id AND p.user_id = $1 AND a.project_id = $2 AND a.app_id = $3
			RETURNING `+columns,
			userID, projectID, id,
			in.Name.Set, in.Name.Value, in.Platform.Set, in.Platform.Value,
			in.BundleID.Set, in.BundleID.Value, in.Environment.Set, in.Environment.Value))
		if err != nil {
			return mapErr(err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &userID, Action: "app.update", EntityType: entityType, EntityID: a.ID, Payload: changes,
		})
	})
	return a, err
}

func (s *Service) Delete(ctx context.Context, userID, projectID, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			DELETE FROM apps a USING projects p
			WHERE p.project_id = a.project_id AND p.user_id = $1 AND a.project_id = $2 AND a.app_id = $3`,
			userID, projectID, id)
		if err != nil {
			return fmt.Errorf("apps: delete: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound(entityType)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &userID, Action: "app.delete", EntityType: entityType, EntityID: id,
		})
	})
}

var envRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,49}$`)

func validateName(v *apperr.Validator, name string) {
	n := utf8.RuneCountInString(name)
	v.Check(n >= 1 && n <= 255, "name", "must be between 1 and 255 characters")
}

func validatePlatform(v *apperr.Validator, p Platform) {
	v.Check(p.Valid(), "platform", "must be one of ios, android, flutter, react_native")
}

func validateBundleID(v *apperr.Validator, id string) {
	n := utf8.RuneCountInString(id)
	v.Check(n >= 1 && n <= 255, "bundle_id", "must be between 1 and 255 characters")
}

func validateEnvironment(v *apperr.Validator, env string) {
	v.Check(envRe.MatchString(env), "environment",
		"must be 1 to 50 lower-case letters, digits, dashes or underscores")
}

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound(entityType)
	}
	if code, constraint, ok := db.PgError(err); ok && code == db.CodeUniqueViolation && constraint == "apps_project_bundle_env_key" {
		return apperr.Conflict("an app with this bundle id and environment already exists in the project")
	}
	return fmt.Errorf("apps: %w", err)
}
