package attachment

import (
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Module provides the attachment-link endpoint, and the Service the worker
// uses to build the links issues show.
var Module = fx.Module("attachment",
	fx.Provide(
		func(q db.Querier) Querier { return q },
		func(cfg *config.Config) *Signer { return NewSigner([]byte(cfg.Auth.JWTSecret)) },
		func(q Querier, signer *Signer, cfg *config.Config) *Service {
			return NewService(q, signer, cfg.Server.PublicURL, cfg.Attachments.LinkTTL)
		},
		httpx.AsRoutes(NewHandler),
	),
)
