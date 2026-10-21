package infra

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

// NewPostgresPool opens a connection pool to Postgres, verifies it with a
// ping, and registers an fx shutdown hook to close it.
func NewPostgresPool(lc fx.Lifecycle, cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("infra: parse database url: %w", err)
	}

	if cfg.Database.MaxConns > 0 {
		poolCfg.MaxConns = cfg.Database.MaxConns
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("infra: create pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("infra: ping database: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			pool.Close()
			return nil
		},
	})

	return pool, nil
}
