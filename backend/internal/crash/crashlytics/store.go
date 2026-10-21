package crashlytics

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secret"
)

// ErrNotConnected is returned when a project has no Crashlytics connection.
var ErrNotConnected = errors.New("crashlytics: project is not connected")

// ErrUnknownProject is returned when a project ID does not exist.
var ErrUnknownProject = errors.New("crashlytics: unknown project")

// Connection is a project's link to Crashlytics, created by the OAuth flow.
type Connection struct {
	ProjectID string
	// Email is the Google account that granted access.
	Email        string
	RefreshToken string
	// Apps are the Firebase apps of the project, as
	// "projects/{project}/apps/{app_id}".
	Apps []string
}

// Store keeps the projects' Crashlytics connections.
type Store interface {
	// OwnsProject reports whether projectID is a project of ownerID.
	OwnsProject(ctx context.Context, ownerID uuid.UUID, projectID string) (bool, error)
	// Save creates or replaces the connection's account and token, keeping
	// its apps.
	Save(ctx context.Context, projectID, email, refreshToken string) error
	// Get returns ErrNotConnected when the project has no connection.
	Get(ctx context.Context, projectID string) (Connection, error)
	List(ctx context.Context) ([]Connection, error)
	// SetApps returns ErrNotConnected when the project has no connection.
	SetApps(ctx context.Context, projectID string, apps []string) error
}

// PGStore is the Store backed by Postgres. Refresh tokens are encrypted
// with box before they are written.
type PGStore struct {
	q   db.Querier
	box *secret.Box
}

var _ Store = (*PGStore)(nil)

func NewPGStore(q db.Querier, box *secret.Box) *PGStore {
	return &PGStore{q: q, box: box}
}

func (s *PGStore) OwnsProject(ctx context.Context, ownerID uuid.UUID, projectID string) (bool, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return false, nil
	}
	_, err := s.q.GetProject(ctx, db.GetProjectParams{ID: id, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("crashlytics: get project: %w", err)
	}
	return true, nil
}

func (s *PGStore) Save(ctx context.Context, projectID, email, refreshToken string) error {
	id, ok := parseUUID(projectID)
	if !ok {
		return ErrUnknownProject
	}
	sealed, err := s.box.Seal([]byte(refreshToken))
	if err != nil {
		return err
	}
	_, err = s.q.UpsertCrashlyticsConnection(ctx, db.UpsertCrashlyticsConnectionParams{
		ProjectID:    id,
		GoogleEmail:  email,
		RefreshToken: sealed,
	})
	if err != nil {
		return fmt.Errorf("crashlytics: save connection: %w", err)
	}
	return nil
}

func (s *PGStore) Get(ctx context.Context, projectID string) (Connection, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return Connection{}, ErrNotConnected
	}
	row, err := s.q.GetCrashlyticsConnection(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotConnected
	}
	if err != nil {
		return Connection{}, fmt.Errorf("crashlytics: get connection: %w", err)
	}
	return s.toConnection(row)
}

func (s *PGStore) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.q.ListCrashlyticsConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("crashlytics: list connections: %w", err)
	}
	conns := make([]Connection, 0, len(rows))
	for _, row := range rows {
		c, err := s.toConnection(row)
		if err != nil {
			return nil, err
		}
		conns = append(conns, c)
	}
	return conns, nil
}

func (s *PGStore) SetApps(ctx context.Context, projectID string, apps []string) error {
	id, ok := parseUUID(projectID)
	if !ok {
		return ErrNotConnected
	}
	n, err := s.q.SetCrashlyticsApps(ctx, db.SetCrashlyticsAppsParams{ProjectID: id, Apps: apps})
	if err != nil {
		return fmt.Errorf("crashlytics: set apps: %w", err)
	}
	if n == 0 {
		return ErrNotConnected
	}
	return nil
}

func (s *PGStore) toConnection(row db.CrashlyticsConnection) (Connection, error) {
	token, err := s.box.Open(row.RefreshToken)
	if err != nil {
		return Connection{}, fmt.Errorf("crashlytics: decrypt refresh token of project %s: %w", row.ProjectID, err)
	}
	return Connection{
		ProjectID:    row.ProjectID.String(),
		Email:        row.GoogleEmail,
		RefreshToken: string(token),
		Apps:         row.Apps,
	}, nil
}

func parseUUID(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	return id, err == nil
}
