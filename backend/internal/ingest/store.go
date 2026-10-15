package ingest

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

// ErrUnauthorized covers unknown and revoked keys alike, so callers can't tell them apart.
var ErrUnauthorized = errors.New("unauthorized")

type Store struct {
	db *pgxpool.Pool
}

func NewStore(ctx context.Context, dsn string) (*Store, error) {
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() {
	s.db.Close()
}

// ProjectFor returns the project that owns a key digest.
func (s *Store) ProjectFor(ctx context.Context, keyHash []byte) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx,
		`SELECT project_id FROM project_keys WHERE key_hash = $1 AND revoked_at IS NULL`,
		keyHash,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrUnauthorized
	}
	return id, err
}

// Save stores the envelope. A repeat of the same id in the same project is a no-op.
// Only the envelope is kept: unknown fields were already dropped on decode.
func (s *Store) Save(ctx context.Context, project uuid.UUID, r Report) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO reports (project_id, id, installation_id, body)
		 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		project, r.ID, r.InstallationID, string(body),
	)
	return err
}
