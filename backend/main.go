package main

import (
	"context"
	"log"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/infra"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("m-seismo backend: load config: %v", err)
	}

	ctx := context.Background()

	pool, err := infra.NewPostgresPool(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("m-seismo backend: connect database: %v", err)
	}
	defer pool.Close()

	log.Printf("m-seismo backend: env=%s listening on %s:%d", cfg.Env, cfg.Server.Host, cfg.Server.Port)
}
