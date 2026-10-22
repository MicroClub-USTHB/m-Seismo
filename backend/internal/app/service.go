// Package app manages apps: the mobile apps that report bugs. An app holds
// the SDK keys, its own tracker connections (the credentials its issues are
// opened with), and is linked to the projects whose repositories receive
// its issues.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
)

const (
	maxNameLen = 200
	// keyPrefix marks m-Seismo keys so they are easy to spot in leaks.
	keyPrefix = "msk_"
	// shownPrefixLen is how much of a key is kept for display.
	shownPrefixLen = len(keyPrefix) + 8
)

type App struct {
	ID        uuid.UUID      `json:"id"`
	Name      string         `json:"name"`
	Platform  db.AppPlatform `json:"platform"`
	BundleID  string         `json:"bundle_id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func toApp(a db.App) App {
	return App{ID: a.ID, Name: a.Name, Platform: a.Platform, BundleID: a.BundleID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// Key is an SDK key. Secret is only set in the response that creates it.
type Key struct {
	ID        uuid.UUID  `json:"id"`
	Prefix    string     `json:"prefix"`
	Secret    string     `json:"secret,omitempty"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func toKey(k db.AppKey) Key {
	return Key{ID: k.ID, Prefix: k.KeyPrefix, RevokedAt: k.RevokedAt, CreatedAt: k.CreatedAt}
}

// LinkedProject is a project an app reports into.
type LinkedProject struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

type Service struct {
	q      db.Querier
	issues *issue.Service
	// limiter and testIssuesPerMinute throttle SendTestIssue per app; a nil
	// limiter or a limit <= 0 disables the throttle.
	limiter             Limiter
	testIssuesPerMinute int
}

// Limiter checks a rate limit. Implemented by *ratelimit.Limiter.
type Limiter interface {
	Allow(ctx context.Context, key string, lim ratelimit.Limit) (ratelimit.Result, error)
}

func NewService(q db.Querier, issues *issue.Service, limiter Limiter, cfg *config.Config) *Service {
	return &Service{q: q, issues: issues, limiter: limiter, testIssuesPerMinute: cfg.Limits.TestIssuesPerMinute}
}

type CreateInput struct {
	Name     string         `json:"name"`
	Platform db.AppPlatform `json:"platform"`
	BundleID string         `json:"bundle_id"`
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, in CreateInput) (App, error) {
	params := db.CreateAppParams{OwnerID: ownerID, Platform: in.Platform}
	var err error
	if params.Name, err = validateName(in.Name); err != nil {
		return App{}, err
	}
	if err := validatePlatform(in.Platform); err != nil {
		return App{}, err
	}
	if params.BundleID, err = validateBundleID(in.BundleID); err != nil {
		return App{}, err
	}
	a, err := s.q.CreateApp(ctx, params)
	if err != nil {
		return App{}, err
	}
	return toApp(a), nil
}

// CreateForProject creates an app and links it to projectID — the "add a
// sub-app" flow from a project page (e.g. a web, iOS or Android build that
// should report into the project's repositories). Not transactional: if
// linking fails after the app is created, the app exists unlinked and can
// still be linked with PUT /apps/{id}/projects/{projectID}.
func (s *Service) CreateForProject(ctx context.Context, ownerID, projectID uuid.UUID, in CreateInput) (App, error) {
	if _, err := s.q.GetProject(ctx, db.GetProjectParams{ID: projectID, OwnerID: ownerID}); err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
			return App{}, apperr.NotFound("project not found")
		}
		return App{}, err
	}
	a, err := s.Create(ctx, ownerID, in)
	if err != nil {
		return App{}, err
	}
	if err := s.q.LinkAppProject(ctx, db.LinkAppProjectParams{AppID: a.ID, ProjectID: projectID}); err != nil {
		return App{}, err
	}
	return a, nil
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]App, error) {
	rows, err := s.q.ListApps(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]App, len(rows))
	for i, a := range rows {
		out[i] = toApp(a)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, ownerID, id uuid.UUID) (App, error) {
	a, err := s.get(ctx, ownerID, id)
	if err != nil {
		return App{}, err
	}
	return toApp(a), nil
}

type UpdateInput struct {
	Name     *string         `json:"name"`
	Platform *db.AppPlatform `json:"platform"`
	BundleID *string         `json:"bundle_id"`
}

func (s *Service) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateInput) (App, error) {
	a, err := s.get(ctx, ownerID, id)
	if err != nil {
		return App{}, err
	}
	params := db.UpdateAppParams{ID: a.ID, OwnerID: ownerID, Name: a.Name, Platform: a.Platform, BundleID: a.BundleID}
	if in.Name != nil {
		if params.Name, err = validateName(*in.Name); err != nil {
			return App{}, err
		}
	}
	if in.Platform != nil {
		if err := validatePlatform(*in.Platform); err != nil {
			return App{}, err
		}
		params.Platform = *in.Platform
	}
	if in.BundleID != nil {
		if params.BundleID, err = validateBundleID(*in.BundleID); err != nil {
			return App{}, err
		}
	}
	updated, err := s.q.UpdateApp(ctx, params)
	if err != nil {
		return App{}, err
	}
	return toApp(updated), nil
}

func (s *Service) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.q.DeleteApp(ctx, db.DeleteAppParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("app not found")
	}
	return nil
}

func (s *Service) ListKeys(ctx context.Context, ownerID, appID uuid.UUID) ([]Key, error) {
	if _, err := s.get(ctx, ownerID, appID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListAppKeys(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := make([]Key, len(rows))
	for i, k := range rows {
		out[i] = toKey(k)
	}
	return out, nil
}

// CreateKey issues a new SDK key. The secret is returned once; only its
// hash is stored.
func (s *Service) CreateKey(ctx context.Context, ownerID, appID uuid.UUID) (Key, error) {
	if _, err := s.get(ctx, ownerID, appID); err != nil {
		return Key{}, err
	}
	secret, err := generateKey()
	if err != nil {
		return Key{}, err
	}
	k, err := s.q.CreateAppKey(ctx, db.CreateAppKeyParams{
		AppID:     appID,
		KeyHash:   hashKey(secret),
		KeyPrefix: secret[:shownPrefixLen],
	})
	if err != nil {
		return Key{}, err
	}
	out := toKey(k)
	out.Secret = secret
	return out, nil
}

// RevokeKey stops a key from being accepted immediately (FR-04).
func (s *Service) RevokeKey(ctx context.Context, ownerID, appID, keyID uuid.UUID) error {
	if _, err := s.get(ctx, ownerID, appID); err != nil {
		return err
	}
	n, err := s.q.RevokeAppKey(ctx, db.RevokeAppKeyParams{ID: keyID, AppID: appID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("active key not found")
	}
	return nil
}

// Authenticate resolves an SDK key to its app. It is the entry point for
// the ingestion API.
func (s *Service) Authenticate(ctx context.Context, rawKey string) (uuid.UUID, error) {
	if !strings.HasPrefix(rawKey, keyPrefix) {
		return uuid.Nil, apperr.Unauthorized("invalid key")
	}
	k, err := s.q.GetActiveAppKeyByHash(ctx, hashKey(rawKey))
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
			return uuid.Nil, apperr.Unauthorized("invalid key")
		}
		return uuid.Nil, err
	}
	return k.AppID, nil
}

func (s *Service) ListProjects(ctx context.Context, ownerID, appID uuid.UUID) ([]LinkedProject, error) {
	if _, err := s.get(ctx, ownerID, appID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListAppProjects(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := make([]LinkedProject, len(rows))
	for i, p := range rows {
		out[i] = LinkedProject{ID: p.ID, Name: p.Name, Slug: p.Slug}
	}
	return out, nil
}

// LinkProject makes the app report into the project. Both must belong to
// the owner. Linking twice is a no-op.
func (s *Service) LinkProject(ctx context.Context, ownerID, appID, projectID uuid.UUID) error {
	if _, err := s.get(ctx, ownerID, appID); err != nil {
		return err
	}
	if _, err := s.q.GetProject(ctx, db.GetProjectParams{ID: projectID, OwnerID: ownerID}); err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
			return apperr.NotFound("project not found")
		}
		return err
	}
	return s.q.LinkAppProject(ctx, db.LinkAppProjectParams{AppID: appID, ProjectID: projectID})
}

func (s *Service) UnlinkProject(ctx context.Context, ownerID, appID, projectID uuid.UUID) error {
	if _, err := s.get(ctx, ownerID, appID); err != nil {
		return err
	}
	n, err := s.q.UnlinkAppProject(ctx, db.UnlinkAppProjectParams{AppID: appID, ProjectID: projectID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("app is not linked to this project")
	}
	return nil
}

func (s *Service) get(ctx context.Context, ownerID, id uuid.UUID) (db.App, error) {
	a, err := s.q.GetApp(ctx, db.GetAppParams{ID: id, OwnerID: ownerID})
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
			return db.App{}, apperr.NotFound("app not found")
		}
		return db.App{}, err
	}
	return a, nil
}

func generateKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("app: generate key: %w", err)
	}
	return keyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > maxNameLen {
		return "", apperr.Invalid("name is required and must be at most 200 characters")
	}
	return name, nil
}

func validatePlatform(p db.AppPlatform) error {
	switch p {
	case db.AppPlatformIos, db.AppPlatformAndroid, db.AppPlatformFlutter, db.AppPlatformReactNative:
		return nil
	}
	return apperr.Invalid("platform must be one of ios, android, flutter, react_native")
}

func validateBundleID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" || len(id) > 255 {
		return "", apperr.Invalid("bundle_id is required and must be at most 255 characters")
	}
	return id, nil
}
