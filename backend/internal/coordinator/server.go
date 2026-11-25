package coordinator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// Authenticator resolves a coordinator key to its app. Implemented by
// *app.Service.
type Authenticator interface {
	AuthenticateCoordinator(ctx context.Context, rawKey string) (uuid.UUID, error)
}

// Server implements Coordinator.Attach.
type Server struct {
	seismov1.UnimplementedCoordinatorServer
	auth Authenticator
	reg  *Registry
}

func NewServer(auth Authenticator, reg *Registry) *Server {
	return &Server{auth: auth, reg: reg}
}

// Attach keeps one coordinator's stream open: it checks the key and the
// Hello, registers the stream for the app, and hands each answer to the
// query waiting for it. It returns when the coordinator closes the stream
// or a newer stream for the same app replaces it.
func (s *Server) Attach(stream seismov1.Coordinator_AttachServer) error {
	ctx := stream.Context()
	appID, err := s.authenticate(ctx)
	if err != nil {
		return err
	}

	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "the first event must be Hello")
	}
	if hello.GetProtocolVersion() != ProtocolVersion {
		return status.Errorf(codes.FailedPrecondition, "protocol version %d is not supported, this backend speaks %d", hello.GetProtocolVersion(), ProtocolVersion)
	}

	c := newConn(appID, hello.GetCoordinatorId(), stream)
	defer c.close()
	if err := s.reg.add(c); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	defer s.reg.remove(c)
	log := slog.With("app", appID, "coordinator", c.coordinatorID)
	log.Info("coordinator attached")

	recvErr := make(chan error, 1)
	go func() {
		for {
			ev, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			c.deliver(ev)
		}
	}()

	select {
	case err := <-recvErr:
		log.Info("coordinator detached", "err", err)
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	case <-c.evicted:
		log.Info("coordinator stream closed by the backend", "reason", c.evictErr)
		return c.evictErr
	}
}

// authenticate reads the coordinator key from the authorization metadata.
func (s *Server) authenticate(ctx context.Context) (uuid.UUID, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	var key string
	if v := md.Get("authorization"); len(v) == 1 {
		key, _ = strings.CutPrefix(v[0], "Bearer ")
	}
	if key == "" {
		return uuid.Nil, status.Error(codes.Unauthenticated, "missing bearer coordinator key")
	}

	appID, err := s.auth.AuthenticateCoordinator(ctx, key)
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindUnauthorized {
			return uuid.Nil, status.Error(codes.Unauthenticated, "invalid coordinator key")
		}
		slog.Error("coordinator: check key", "err", err)
		return uuid.Nil, status.Error(codes.Unavailable, "cannot check the key right now")
	}
	return appID, nil
}
