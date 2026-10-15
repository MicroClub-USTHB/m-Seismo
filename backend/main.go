package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL is not set")
		os.Exit(1)
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	store, err := ingest.NewStore(context.Background(), dsn)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}

	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, ingest.NewHandler(store)); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
