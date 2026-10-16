// Package infra wires up connections to external systems: the database,
// Redis, and NATS JetStream (queue management). Later: object storage, the
// GitHub client, etc.
package infra

import "go.uber.org/fx"

// Module provides the infra-layer dependencies: a Postgres pool, a Redis
// client, and a NATS connection with its JetStream context. Each constructor
// registers its own fx shutdown hook.
var Module = fx.Module("infra",
	fx.Provide(
		NewPostgresPool,
		NewRedisClient,
		NewNATSConn,
		NewJetStream,
	),
)
