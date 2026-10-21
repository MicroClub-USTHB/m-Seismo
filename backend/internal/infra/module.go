// Package infra wires up connections to external systems: the database,
// Redis, and NATS JetStream (queue management). Later: object storage, etc.
package infra

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// Module provides the infra-layer dependencies: a Postgres pool and the
// sqlc queries on top of it, a Redis client, and a NATS connection with its
// JetStream context. Each constructor registers its own fx shutdown hook.
var Module = fx.Module("infra",
	fx.Provide(
		NewPostgresPool,
		NewQueries,
		func(q *db.Queries) db.Querier { return q },
		NewRedisClient,
		NewNATSConn,
		NewJetStream,
	),
)

// NewQueries wraps the pool in sqlc's generated queries. Use
// Queries.WithTx for work that must be transactional.
func NewQueries(pool *pgxpool.Pool) *db.Queries {
	return db.New(pool)
}
