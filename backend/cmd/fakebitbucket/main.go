// Command fakebitbucket serves the development-only Bitbucket Cloud stand-in described
// in package fakebitbucket, so the Bitbucket Cloud connect flow and issue opening work
// without real OAuth consumer credentials.
//
//	FAKEBITBUCKET_ADDR        listen address (default :9092)
//	FAKEBITBUCKET_PUBLIC_URL  base of the issue URLs it returns (default http://localhost:9092)
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/bitbucket/fakebitbucket"
)

func main() {
	addr := env("FAKEBITBUCKET_ADDR", ":9092")
	srv := fakebitbucket.New(env("FAKEBITBUCKET_PUBLIC_URL", "http://localhost:9092"))
	log.Printf("fakebitbucket: listening on %s", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Fatal(server.ListenAndServe())
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
