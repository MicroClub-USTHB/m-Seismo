//go:build e2e

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// TestOpenForAppE2E opens a real issue through the full graph: Postgres,
// Redis, NATS and a configured GitHub App must be reachable, and
// SEISMO_E2E_APP_ID must name an app linked to a project whose default
// repository the app reaches through one of its own connections (check with
// GET /api/v1/apps/{id}/targets).
//
//	SEISMO_E2E_APP_ID=<uuid> go test -tags e2e -run TestOpenForAppE2E .
func TestOpenForAppE2E(t *testing.T) {
	appID, err := uuid.Parse(os.Getenv("SEISMO_E2E_APP_ID"))
	if err != nil {
		t.Skip("SEISMO_E2E_APP_ID not set")
	}

	var svc *issue.Service
	app := fxtest.New(t, Options(), fx.Populate(&svc))
	app.RequireStart()
	defer app.RequireStop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := svc.OpenForApp(ctx, appID, issue.Draft{
		Title:       "m-Seismo e2e test issue " + time.Now().Format(time.RFC3339),
		Body:        "Opened by the m-Seismo e2e test. Safe to close.",
		Labels:      []string{"m-seismo-e2e"},
		Fingerprint: "e2e",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("%s %s: %v", r.Tracker, r.Repository, r.Err)
			continue
		}
		t.Logf("opened %s (%s) through connection %s, recorded as %s", r.Ref.ID, r.Ref.URL, r.ConnectionID, r.IssueID)
	}
}
