package main

import (
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/app"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/auth"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/infra"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/owner"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/project"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/github"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/worker"
)

func main() {
	fx.New(Options()).Run()
}

// Options is the full application graph, shared with the e2e tests.
func Options() fx.Option {
	return fx.Options(
		config.Module,
		infra.Module,
		httpx.Module,
		auth.Module,
		owner.Module,
		project.Module,
		app.Module,
		secrets.Module,
		provider.Module,
		github.Module,
		issue.Module,
		ratelimit.Module,
		ingest.Module,
		worker.Module,
	)
}
