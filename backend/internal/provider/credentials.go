package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
)

// ErrNoCredentials means the app has not registered an OAuth application
// for the tracker yet.
var ErrNoCredentials = errors.New("provider: app has no OAuth application for this tracker")

// Credentials is one app's OAuth application for a tracker that is reached
// with per-connection OAuth tokens (GitLab, Bitbucket). Each app brings its
// own, instead of one shared application configured for the whole
// deployment, so owners never have to trust m-Seismo's operator with a
// single OAuth application that reaches every connected account.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// CredentialStore keeps each app's tracker OAuth application. The client
// secret is encrypted at rest.
type CredentialStore interface {
	// Get returns the app's OAuth application for tracker, or
	// ErrNoCredentials.
	Get(ctx context.Context, appID uuid.UUID, tracker db.Tracker) (Credentials, error)
	// Save replaces the app's OAuth application for tracker.
	Save(ctx context.Context, appID uuid.UUID, tracker db.Tracker, creds Credentials) error
}

type dbCredentialStore struct {
	q      db.Querier
	cipher *secrets.Cipher
}

// NewDBCredentialStore stores each app's tracker client_secret in
// app_tracker_credentials, sealed with AES-256-GCM. The app ID and tracker
// are the associated data, so a ciphertext moved to another app's row, or
// to another tracker, does not decrypt.
func NewDBCredentialStore(q db.Querier, cipher *secrets.Cipher) CredentialStore {
	return &dbCredentialStore{q: q, cipher: cipher}
}

func associatedData(appID uuid.UUID, tracker db.Tracker) []byte {
	return append(appID[:], []byte(tracker)...)
}

func (s *dbCredentialStore) Get(ctx context.Context, appID uuid.UUID, tracker db.Tracker) (Credentials, error) {
	row, err := s.q.GetAppTrackerCredentials(ctx, db.GetAppTrackerCredentialsParams{AppID: appID, Tracker: tracker})
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrNoCredentials
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("provider: load %s credentials: %w", tracker, err)
	}
	secret, err := s.cipher.Open(row.ClientSecretCiphertext, associatedData(appID, tracker))
	if err != nil {
		return Credentials{}, fmt.Errorf("provider: open %s credentials of app %s: %w", tracker, appID, err)
	}
	return Credentials{ClientID: row.ClientID, ClientSecret: string(secret)}, nil
}

func (s *dbCredentialStore) Save(ctx context.Context, appID uuid.UUID, tracker db.Tracker, creds Credentials) error {
	sealed, err := s.cipher.Seal([]byte(creds.ClientSecret), associatedData(appID, tracker))
	if err != nil {
		return fmt.Errorf("provider: seal %s credentials: %w", tracker, err)
	}
	if _, err := s.q.UpsertAppTrackerCredentials(ctx, db.UpsertAppTrackerCredentialsParams{
		AppID:                  appID,
		Tracker:                tracker,
		ClientID:               creds.ClientID,
		ClientSecretCiphertext: sealed,
	}); err != nil {
		return fmt.Errorf("provider: save %s credentials: %w", tracker, err)
	}
	return nil
}
