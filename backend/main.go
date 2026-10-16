package main

import (
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/infra"
)

func main() {
	fx.New(
		config.Module,
		infra.Module,
		fx.Invoke(run),
	).Run()
}

func run(cfg *config.Config, _ *pgxpool.Pool, _ *redis.Client, _ jetstream.JetStream) {
	log.Printf("m-seismo backend: env=%s listening on %s:%d", cfg.Env, cfg.Server.Host, cfg.Server.Port)
}
