// Command fakegitlab serves the development-only GitLab stand-in described
// in package fakegitlab, so the GitLab connect flow and issue opening work
// without real GitLab application credentials.
//
//	FAKEGITLAB_ADDR        listen address (default :9091)
//	FAKEGITLAB_PUBLIC_URL  base of the issue URLs it returns (default http://localhost:9091)
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/gitlab/fakegitlab"
)

func main() {
	addr := env("FAKEGITLAB_ADDR", ":9091")
	srv := fakegitlab.New(env("FAKEGITLAB_PUBLIC_URL", "http://localhost:9091"))
	log.Printf("fakegitlab: listening on %s", addr)
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
