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
)

const (
	stateTTL       = 10 * time.Minute
	stateKeyPrefix = "provider_state:"
)

// Connection is the public view of a provider connection.
type Connection struct {
	ID           uuid.UUID  `json:"id"`
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
		Tracker:      c.Tracker,
		AccountLogin: c.AccountLogin,
		AccountType:  c.AccountType,
		SuspendedAt:  c.SuspendedAt,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
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

// Service runs the connect flow and manages an owner's connections.
type Service struct {
	q      db.Querier
	reg    *Registry
	states StateStore
}

func NewService(q db.Querier, reg *Registry, states StateStore) *Service {
	return &Service{q: q, reg: reg, states: states}
}

// StartConnect records a one-time state for the owner and returns the URL
// that starts the tracker's install/authorize flow.
func (s *Service) StartConnect(ctx context.Context, ownerID uuid.UUID, tracker db.Tracker) (string, error) {
	p, err := s.reg.Get(tracker)
	if err != nil {
		return "", err
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("provider: generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)

	value := string(tracker) + ":" + ownerID.String()
	if err := s.states.Put(ctx, stateKeyPrefix+state, value, stateTTL); err != nil {
		return "", fmt.Errorf("provider: store state: %w", err)
	}
	return p.InstallURL(state), nil
}

// FinishConnect handles the tracker's callback: it consumes the state,
// lets the provider verify the grant, and saves the connection for the
// owner who started the flow.
func (s *Service) FinishConnect(ctx context.Context, tracker db.Tracker, query url.Values) (Connection, error) {
	p, err := s.reg.Get(tracker)
	if err != nil {
		return Connection{}, err
	}

	state := query.Get("state")
	if state == "" {
		return Connection{}, apperr.Invalid("missing state")
	}
	value, ok, err := s.states.Take(ctx, stateKeyPrefix+state)
	if err != nil {
		return Connection{}, fmt.Errorf("provider: read state: %w", err)
	}
	if !ok {
		return Connection{}, apperr.Invalid("unknown or expired state")
	}
	stateTracker, rawOwner, _ := strings.Cut(value, ":")
	ownerID, err := uuid.Parse(rawOwner)
	if err != nil || stateTracker != string(tracker) {
		return Connection{}, apperr.Invalid("state does not match this provider")
	}

	acct, err := p.CompleteInstall(ctx, query)
	if err != nil {
		return Connection{}, err
	}

	c, err := s.q.UpsertProviderConnection(ctx, db.UpsertProviderConnectionParams{
		OwnerID:      ownerID,
		Tracker:      tracker,
		ExternalID:   acct.ExternalID,
		AccountLogin: acct.Login,
		AccountType:  acct.Type,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, apperr.Conflict("this account is already connected to another owner")
	}
	if err != nil {
		return Connection{}, err
	}
	return toConnection(c), nil
}

func (s *Service) ListConnections(ctx context.Context, ownerID uuid.UUID) ([]Connection, error) {
	rows, err := s.q.ListProviderConnections(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]Connection, len(rows))
	for i, c := range rows {
		out[i] = toConnection(c)
	}
	return out, nil
}

// Disconnect forgets the connection and every project repository linked
// through it. It does not uninstall anything on the tracker side.
func (s *Service) Disconnect(ctx context.Context, ownerID, connectionID uuid.UUID) error {
	n, err := s.q.DeleteProviderConnection(ctx, db.DeleteProviderConnectionParams{ID: connectionID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("connection not found")
	}
	return nil
}

// ListRepositories lists the repositories the owner's connection can reach.
func (s *Service) ListRepositories(ctx context.Context, ownerID, connectionID uuid.UUID) ([]Repository, error) {
	c, p, err := s.connection(ctx, ownerID, connectionID)
	if err != nil {
		return nil, err
	}
	return p.ListRepositories(ctx, c.ExternalID)
}

// VerifyRepository checks that the connection belongs to the owner and can
// reach repository. Used before a repository is linked to a project.
func (s *Service) VerifyRepository(ctx context.Context, ownerID, connectionID uuid.UUID, repository string) (Connection, error) {
	c, p, err := s.connection(ctx, ownerID, connectionID)
	if err != nil {
		return Connection{}, err
	}
	ok, err := p.HasRepository(ctx, c.ExternalID, repository)
	if err != nil {
		return Connection{}, err
	}
	if !ok {
		return Connection{}, apperr.Invalid("repository " + repository + " is not reachable through this connection")
	}
	return toConnection(c), nil
}

func (s *Service) connection(ctx context.Context, ownerID, connectionID uuid.UUID) (db.ProviderConnection, Provider, error) {
	c, err := s.q.GetProviderConnection(ctx, db.GetProviderConnectionParams{ID: connectionID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ProviderConnection{}, nil, apperr.NotFound("connection not found")
	}
	if err != nil {
		return db.ProviderConnection{}, nil, err
	}
	if c.SuspendedAt != nil {
		return db.ProviderConnection{}, nil, apperr.Invalid("connection is suspended")
	}
	p, err := s.reg.Get(c.Tracker)
	if err != nil {
		return db.ProviderConnection{}, nil, err
	}
	return c, p, nil
}
