package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

const (
	stateTTL       = 10 * time.Minute
	stateKeyPrefix = "provider_state:"
)

// Connection is the public view of a provider connection. Every connection
// belongs to one app.
type Connection struct {
	ID           uuid.UUID  `json:"id"`
	AppID        uuid.UUID  `json:"app_id"`
	Tracker      db.Tracker `json:"tracker"`
	AccountLogin string     `json:"account_login"`
	AccountType  string     `json:"account_type"`
	SuspendedAt  *time.Time `json:"suspended_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func toConnection(c db.ProviderConnection) Connection {
	return Connection{
		ID:           c.ID,
		AppID:        c.AppID,
		Tracker:      c.Tracker,
		AccountLogin: c.AccountLogin,
		AccountType:  c.AccountType,
		SuspendedAt:  c.SuspendedAt,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

func toConnections(rows []db.ProviderConnection) []Connection {
	out := make([]Connection, len(rows))
	for i, c := range rows {
		out[i] = toConnection(c)
	}
	return out
}

// StateStore keeps connect-flow states between the redirect to the tracker
// and its callback. Implemented with Redis in production.
type StateStore interface {
	Put(ctx context.Context, key, value string, ttl time.Duration) error
	// Take returns and deletes the value; ok is false when it is missing or
	// expired.
	Take(ctx context.Context, key string) (value string, ok bool, err error)
}

type redisStateStore struct{ rdb *redis.Client }

func NewRedisStateStore(rdb *redis.Client) StateStore { return redisStateStore{rdb: rdb} }

func (s redisStateStore) Put(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.rdb.Set(ctx, key, value, ttl).Err()
}

func (s redisStateStore) Take(ctx context.Context, key string) (string, bool, error) {
	v, err := s.rdb.GetDel(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Service runs the connect flow and manages each app's connections. Every
// method is scoped to the owner, and all but ListOwnerConnections to one of
// the owner's apps.
type Service struct {
	q      db.Querier
	reg    *Registry
	states StateStore
}

func NewService(q db.Querier, reg *Registry, states StateStore) *Service {
	return &Service{q: q, reg: reg, states: states}
}

// connectState is what a state token stands for: who started the flow, for
// which app, on which tracker.
type connectState struct {
	tracker db.Tracker
	ownerID uuid.UUID
	appID   uuid.UUID
}

func (c connectState) encode() string {
	return string(c.tracker) + ":" + c.ownerID.String() + ":" + c.appID.String()
}

func decodeState(v string) (connectState, bool) {
	parts := strings.Split(v, ":")
	if len(parts) != 3 {
		return connectState{}, false
	}
	ownerID, err1 := uuid.Parse(parts[1])
	appID, err2 := uuid.Parse(parts[2])
	if err1 != nil || err2 != nil {
		return connectState{}, false
	}
	return connectState{tracker: db.Tracker(parts[0]), ownerID: ownerID, appID: appID}, true
}

// StartConnect records a one-time state for the owner's app and returns the
// URL that starts the tracker's install/authorize flow.
func (s *Service) StartConnect(ctx context.Context, ownerID, appID uuid.UUID, tracker db.Tracker) (string, error) {
	p, err := s.reg.Get(tracker)
	if err != nil {
		return "", err
	}
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return "", err
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("provider: generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)

	value := connectState{tracker: tracker, ownerID: ownerID, appID: appID}.encode()
	if err := s.states.Put(ctx, stateKeyPrefix+state, value, stateTTL); err != nil {
		return "", fmt.Errorf("provider: store state: %w", err)
	}
	return p.InstallURL(state), nil
}

// FinishConnect handles the tracker's callback: it consumes the state, lets
// the provider verify the grant, and saves the connection on the app the
// flow was started for. appID is returned whenever the state was valid,
// even with an error, so the caller can send the browser back to that app.
func (s *Service) FinishConnect(ctx context.Context, tracker db.Tracker, query url.Values) (appID uuid.UUID, conn Connection, err error) {
	p, err := s.reg.Get(tracker)
	if err != nil {
		return uuid.Nil, Connection{}, err
	}

	state := query.Get("state")
	if state == "" {
		return uuid.Nil, Connection{}, apperr.Invalid("missing state")
	}
	value, ok, err := s.states.Take(ctx, stateKeyPrefix+state)
	if err != nil {
		return uuid.Nil, Connection{}, fmt.Errorf("provider: read state: %w", err)
	}
	if !ok {
		return uuid.Nil, Connection{}, apperr.Invalid("unknown or expired state")
	}
	st, ok := decodeState(value)
	if !ok || st.tracker != tracker {
		return uuid.Nil, Connection{}, apperr.Invalid("state does not match this provider")
	}

	acct, err := p.CompleteInstall(ctx, query)
	if err != nil {
		return st.appID, Connection{}, err
	}

	c, err := s.upsert(ctx, st.ownerID, st.appID, tracker, acct)
	if err != nil {
		return st.appID, Connection{}, err
	}
	return st.appID, c, nil
}

// AttachConnection gives the app a connection the owner already verified on
// another of their apps, without a new round trip through the tracker. With
// GitHub this is the way to share an installation between apps: installing
// an App that is already installed only shows its settings page.
func (s *Service) AttachConnection(ctx context.Context, ownerID, appID, fromConnectionID uuid.UUID) (Connection, error) {
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return Connection{}, err
	}
	src, err := s.q.GetOwnerConnection(ctx, db.GetOwnerConnectionParams{ID: fromConnectionID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, apperr.NotFound("connection not found")
	}
	if err != nil {
		return Connection{}, err
	}
	if src.SuspendedAt != nil {
		return Connection{}, apperr.Invalid("connection is suspended")
	}
	if _, err := s.reg.Get(src.Tracker); err != nil {
		return Connection{}, err
	}
	return s.upsert(ctx, ownerID, appID, src.Tracker, Account{
		ExternalID: src.ExternalID,
		Login:      src.AccountLogin,
		Type:       src.AccountType,
	})
}

func (s *Service) upsert(ctx context.Context, ownerID, appID uuid.UUID, tracker db.Tracker, acct Account) (Connection, error) {
	c, err := s.q.UpsertProviderConnection(ctx, db.UpsertProviderConnectionParams{
		OwnerID:      ownerID,
		AppID:        appID,
		Tracker:      tracker,
		ExternalID:   acct.ExternalID,
		AccountLogin: acct.Login,
		AccountType:  acct.Type,
	})
	if err != nil {
		switch kind, _ := apperr.Classify(err); kind {
		case apperr.KindConflict:
			return Connection{}, apperr.Conflict("this " + string(tracker) + " account is already connected by another owner")
		case apperr.KindInvalid:
			// The app was deleted while the flow was running.
			return Connection{}, apperr.NotFound("app not found")
		}
		return Connection{}, err
	}
	return toConnection(c), nil
}

// ListConnections lists the app's connections.
func (s *Service) ListConnections(ctx context.Context, ownerID, appID uuid.UUID) ([]Connection, error) {
	if err := s.requireApp(ctx, ownerID, appID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListAppConnections(ctx, db.ListAppConnectionsParams{AppID: appID, OwnerID: ownerID})
	if err != nil {
		return nil, err
	}
	return toConnections(rows), nil
}

// ListOwnerConnections lists the connections of all the owner's apps, for
// choosing one to attach to another app.
func (s *Service) ListOwnerConnections(ctx context.Context, ownerID uuid.UUID) ([]Connection, error) {
	rows, err := s.q.ListOwnerConnections(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return toConnections(rows), nil
}

// Disconnect removes the connection from the app. Other apps sharing the
// same tracker account keep theirs, project repositories are untouched,
// and nothing is uninstalled on the tracker side.
func (s *Service) Disconnect(ctx context.Context, ownerID, appID, connectionID uuid.UUID) error {
	n, err := s.q.DeleteAppConnection(ctx, db.DeleteAppConnectionParams{ID: connectionID, AppID: appID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("connection not found")
	}
	return nil
}

// ListRepositories lists the repositories the app's connection can reach.
func (s *Service) ListRepositories(ctx context.Context, ownerID, appID, connectionID uuid.UUID) ([]Repository, error) {
	c, err := s.q.GetAppConnection(ctx, db.GetAppConnectionParams{ID: connectionID, AppID: appID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("connection not found")
	}
	if err != nil {
		return nil, err
	}
	if c.SuspendedAt != nil {
		return nil, apperr.Invalid("connection is suspended")
	}
	p, err := s.reg.Get(c.Tracker)
	if err != nil {
		return nil, err
	}
	return p.ListRepositories(ctx, issue.ConnectionFromRow(c))
}

func (s *Service) requireApp(ctx context.Context, ownerID, appID uuid.UUID) error {
	_, err := s.q.GetApp(ctx, db.GetAppParams{ID: appID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("app not found")
	}
	return err
}
