package infra

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

// NewNATSConn opens a connection to NATS and registers an fx shutdown hook
// to close it.
func NewNATSConn(lc fx.Lifecycle, cfg *config.Config) (*nats.Conn, error) {
	nc, err := nats.Connect(cfg.NATS.URL)
	if err != nil {
		return nil, fmt.Errorf("infra: connect nats: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			nc.Close()
			return nil
		},
	})

	return nc, nil
}

// NewJetStream wraps a NATS connection with the JetStream context used for
// queue management (streams, consumers, publish/consume).
func NewJetStream(nc *nats.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("infra: create jetstream context: %w", err)
	}

	return js, nil
}
