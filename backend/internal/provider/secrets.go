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

// ErrNoSecret is returned by SecretStore.Load for a connection without
// stored credentials, such as every GitHub connection.
var ErrNoSecret = errors.New("provider: connection has no stored credentials")

// SecretStore keeps the credentials of a connection (for GitLab and
// Bitbucket, its OAuth tokens) as opaque bytes. Providers choose the
// encoding; the store encrypts them at rest.
type SecretStore interface {
	// Load returns the connection's credentials, or ErrNoSecret.
	Load(ctx context.Context, connectionID uuid.UUID) ([]byte, error)
	// Save replaces the connection's credentials.
	Save(ctx context.Context, connectionID uuid.UUID, secret []byte) error
}

type dbSecretStore struct {
	q      db.Querier
	cipher *secrets.Cipher
}

// NewDBSecretStore stores credentials in connection_secrets, sealed with
// AES-256-GCM. The connection ID is the associated data, so a ciphertext
// moved to another connection's row does not decrypt.
func NewDBSecretStore(q db.Querier, cipher *secrets.Cipher) SecretStore {
	return &dbSecretStore{q: q, cipher: cipher}
}

func (s *dbSecretStore) Load(ctx context.Context, connectionID uuid.UUID) ([]byte, error) {
	sealed, err := s.q.GetConnectionSecret(ctx, connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSecret
	}
	if err != nil {
		return nil, fmt.Errorf("provider: load secret: %w", err)
	}
	plain, err := s.cipher.Open(sealed, connectionID[:])
	if err != nil {
		return nil, fmt.Errorf("provider: open secret of connection %s: %w", connectionID, err)
	}
	return plain, nil
}

func (s *dbSecretStore) Save(ctx context.Context, connectionID uuid.UUID, secret []byte) error {
	sealed, err := s.cipher.Seal(secret, connectionID[:])
	if err != nil {
		return fmt.Errorf("provider: seal secret: %w", err)
	}
	if err := s.q.UpsertConnectionSecret(ctx, db.UpsertConnectionSecretParams{
		ProviderConnectionID: connectionID,
		Ciphertext:           sealed,
	}); err != nil {
		return fmt.Errorf("provider: save secret: %w", err)
	}
	return nil
}
