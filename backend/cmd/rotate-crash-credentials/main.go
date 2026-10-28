// rotate-crash-credentials rewrites Sentry tokens using the configured active
// encryption key. Supply old keys through previous_encryption_keys until all
// backend instances and persisted credentials have been migrated.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crashprovider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func run() error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer pool.Close()
	service, err := crashprovider.NewService(db.New(pool), cfg)
	if err != nil {
		return err
	}
	count, err := service.RotateCredentials(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Re-encrypted %d Sentry connection(s).\n", count)
	return nil
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
