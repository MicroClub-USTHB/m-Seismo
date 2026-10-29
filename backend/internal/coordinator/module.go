package coordinator

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/app"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// Module provides the Client every instance uses to ask for server logs,
// and serves Coordinator.Attach when coordinator.enabled is set.
var Module = fx.Module("coordinator",
	fx.Provide(
		func(s *app.Service) Authenticator { return s },
		newClient,
	),
	fx.Invoke(register),
)

func newClient(nc *nats.Conn, cfg *config.Config) *Client {
	c := cfg.Coordinator
	return NewClient(nc, c.Timeout, Limits{MaxBytes: c.MaxBytes, MaxLinesPerWorkload: c.MaxLinesPerWorkload})
}

func register(lc fx.Lifecycle, cfg *config.Config, nc *nats.Conn, auth Authenticator) error {
	c := cfg.Coordinator
	if !c.Enabled {
		return nil
	}

	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxMessageBytes),
		grpc.MaxSendMsgSize(maxMessageBytes),
		// Pings find dead coordinators, so a stale stream doesn't keep
		// its app's queries waiting.
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
	}
	if c.TLSCertFile != "" {
		cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("coordinator: load TLS certificate: %w", err)
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})))
	}

	reg := NewRegistry(nc)
	srv := grpc.NewServer(opts...)
	seismov1.RegisterCoordinatorServer(srv, NewServer(auth, reg))

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := reg.Start(); err != nil {
				return err
			}
			lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", c.Addr)
			if err != nil {
				return fmt.Errorf("coordinator: listen on %s: %w", c.Addr, err)
			}
			go func() {
				if err := srv.Serve(lis); err != nil {
					slog.Error("coordinator: gRPC server stopped", "err", err)
				}
			}()
			slog.Info("coordinator gRPC server listening", "addr", c.Addr, "tls", c.TLSCertFile != "")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			// Attach streams last as long as the coordinator is up, so they
			// are closed rather than waited for; coordinators reconnect to
			// another instance.
			reg.Stop()
			done := make(chan struct{})
			go func() { srv.GracefulStop(); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				srv.Stop()
			}
			return nil
		},
	})
	return nil
}
