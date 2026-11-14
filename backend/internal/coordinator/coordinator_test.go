package coordinator

import (
	"context"
	"errors"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/testenv"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// These tests need a NATS server: a container, or the one in
// SEISMO_TEST_NATS_URL.

type fakeAuth map[string]uuid.UUID

func (f fakeAuth) AuthenticateCoordinator(_ context.Context, key string) (uuid.UUID, error) {
	if id, ok := f[key]; ok {
		return id, nil
	}
	return uuid.Nil, apperr.Unauthorized("invalid key")
}

// instance is one backend instance: a gRPC server and its registry.
type instance struct {
	lis *bufconn.Listener
}

func newInstance(t *testing.T, nc *nats.Conn, auth Authenticator) *instance {
	t.Helper()
	reg := NewRegistry(nc)
	if err := reg.Start(); err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	seismov1.RegisterCoordinatorServer(srv, NewServer(auth, reg))
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { reg.Stop(); srv.Stop() })
	return &instance{lis: lis}
}

// fakeCoordinator attaches with key and answers every query with answer,
// or never answers when answer is nil.
type fakeCoordinator struct {
	stream seismov1.Coordinator_AttachClient
	ended  chan error
}

func attach(t *testing.T, in *instance, key string, hello *seismov1.Hello, answer func(*seismov1.QueryLogs) *seismov1.CoordinatorEvent) *fakeCoordinator {
	t.Helper()
	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return in.lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+key)
	stream, err := seismov1.NewCoordinatorClient(cc).Attach(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hello != nil {
		if err := stream.Send(&seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Hello{Hello: hello}}); err != nil {
			t.Fatal(err)
		}
	}

	f := &fakeCoordinator{stream: stream, ended: make(chan error, 1)}
	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				f.ended <- err
				return
			}
			if answer == nil {
				continue
			}
			if ev := answer(req.GetQueryLogs()); ev != nil {
				_ = stream.Send(ev)
			}
		}
	}()
	return f
}

// waitEnd returns the status the backend ended the stream with.
func (f *fakeCoordinator) waitEnd(t *testing.T) codes.Code {
	t.Helper()
	select {
	case err := <-f.ended:
		return status.Code(err)
	case <-time.After(5 * time.Second):
		t.Fatal("stream still open")
		return codes.OK
	}
}

func answerAs(agentID string) func(*seismov1.QueryLogs) *seismov1.CoordinatorEvent {
	return func(q *seismov1.QueryLogs) *seismov1.CoordinatorEvent {
		return &seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Result{Result: &seismov1.AggregateResult{
			SchemaVersion:   1,
			QueryId:         q.GetQueryId(),
			RequestedWindow: q.GetWindow(),
			Agents:          []*seismov1.AgentSection{{AgentId: agentID, Status: seismov1.AgentStatus_AGENT_STATUS_OK}},
		}}}
	}
}

func hello() *seismov1.Hello {
	return &seismov1.Hello{ProtocolVersion: ProtocolVersion, CoordinatorId: "test"}
}

func natsConn(t *testing.T) *nats.Conn {
	t.Helper()
	url := testenv.NATSURL(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// queryUntil retries while the attachment propagates through NATS.
func queryUntil(t *testing.T, c *Client, appID uuid.UUID, want string) {
	t.Helper()
	var err error
	for i := 0; i < 50; i++ {
		var res *seismov1.AggregateResult
		res, err = c.QueryLogs(context.Background(), appID, time.Now().Add(-time.Minute), time.Now())
		if err == nil && len(res.GetAgents()) == 1 && res.GetAgents()[0].GetAgentId() == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no answer from %s: %v", want, err)
}

func TestQueryReachesTheAttachedCoordinator(t *testing.T) {
	nc := natsConn(t)
	appID := uuid.New()
	in := newInstance(t, nc, fakeAuth{"mco_good": appID})
	client := NewClient(nc, 2*time.Second, Limits{MaxBytes: 256 << 10, MaxLinesPerWorkload: 100})

	if _, err := client.QueryLogs(context.Background(), appID, time.Now().Add(-time.Minute), time.Now()); !errors.Is(err, ErrNotAttached) {
		t.Fatalf("before attach: err = %v, want ErrNotAttached", err)
	}

	queries := make(chan *seismov1.QueryLogs, 100)
	attach(t, in, "mco_good", hello(), func(q *seismov1.QueryLogs) *seismov1.CoordinatorEvent {
		queries <- q
		return answerAs("agent-1")(q)
	})
	queryUntil(t, client, appID, "agent-1")

	got := <-queries
	if got.GetLimits().GetMaxBytesTotal() != 256<<10 || got.GetLimits().GetMaxLinesPerWorkload() != 100 {
		t.Errorf("limits = %v", got.GetLimits())
	}
	if !got.GetDeadline().AsTime().After(time.Now()) {
		t.Errorf("deadline %v is not in the future", got.GetDeadline().AsTime())
	}
}

func TestBudgetFitsInOneNATSMessage(t *testing.T) {
	nc := natsConn(t)
	appID := uuid.New()
	in := newInstance(t, nc, fakeAuth{"mco_good": appID})
	client := NewClient(nc, 2*time.Second, Limits{MaxBytes: 8 << 20, MaxLinesPerWorkload: 100})

	queries := make(chan *seismov1.QueryLogs, 100)
	attach(t, in, "mco_good", hello(), func(q *seismov1.QueryLogs) *seismov1.CoordinatorEvent {
		queries <- q
		return answerAs("agent-1")(q)
	})
	queryUntil(t, client, appID, "agent-1")

	if got, max := int64((<-queries).GetLimits().GetMaxBytesTotal()), nc.MaxPayload()-payloadOverhead; got != max {
		t.Errorf("max_bytes_total = %d, want the NATS max payload less the overhead, %d", got, max)
	}
}

func TestAttachRejections(t *testing.T) {
	nc := natsConn(t)
	in := newInstance(t, nc, fakeAuth{"mco_good": uuid.New()})

	tests := []struct {
		name  string
		key   string
		hello *seismov1.Hello
		first *seismov1.CoordinatorEvent
		want  codes.Code
	}{
		{name: "bad key", key: "mco_bad", hello: hello(), want: codes.Unauthenticated},
		{name: "no key", key: "", hello: hello(), want: codes.Unauthenticated},
		{name: "unsupported version", key: "mco_good", hello: &seismov1.Hello{ProtocolVersion: 2}, want: codes.FailedPrecondition},
		{name: "first event is not Hello", key: "mco_good", first: &seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Result{Result: &seismov1.AggregateResult{}}}, want: codes.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := attach(t, in, tt.key, tt.hello, nil)
			if tt.first != nil {
				_ = f.stream.Send(tt.first)
			}
			if got := f.waitEnd(t); got != tt.want {
				t.Errorf("status = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNewerAttachReplacesTheOlderOne(t *testing.T) {
	nc := natsConn(t)
	appID := uuid.New()
	auth := fakeAuth{"mco_good": appID}
	a, b := newInstance(t, nc, auth), newInstance(t, nc, auth)
	client := NewClient(nc, 2*time.Second, Limits{MaxBytes: 1 << 20, MaxLinesPerWorkload: 100})

	t.Run("on the same instance", func(t *testing.T) {
		first := attach(t, a, "mco_good", hello(), answerAs("first"))
		queryUntil(t, client, appID, "first")
		attach(t, a, "mco_good", hello(), answerAs("second"))
		if got := first.waitEnd(t); got != codes.Aborted {
			t.Errorf("older stream ended with %s, want Aborted", got)
		}
		queryUntil(t, client, appID, "second")
	})

	t.Run("on another instance", func(t *testing.T) {
		first := attach(t, a, "mco_good", hello(), answerAs("on-a"))
		queryUntil(t, client, appID, "on-a")
		attach(t, b, "mco_good", hello(), answerAs("on-b"))
		if got := first.waitEnd(t); got != codes.Aborted {
			t.Errorf("stream on the other instance ended with %s, want Aborted", got)
		}
		queryUntil(t, client, appID, "on-b")
	})
}

func TestSilentCoordinatorTimesOut(t *testing.T) {
	nc := natsConn(t)
	appID := uuid.New()
	in := newInstance(t, nc, fakeAuth{"mco_good": appID})
	client := NewClient(nc, 300*time.Millisecond, Limits{MaxBytes: 1 << 20, MaxLinesPerWorkload: 100})

	attach(t, in, "mco_good", hello(), nil)
	start := time.Now()
	var err error
	for i := 0; i < 50; i++ {
		_, err = client.QueryLogs(context.Background(), appID, time.Now().Add(-time.Minute), time.Now())
		if !errors.Is(err, ErrNotAttached) {
			break
		}
		time.Sleep(50 * time.Millisecond)
		start = time.Now()
	}
	if err == nil || errors.Is(err, ErrNotAttached) {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("took %s to give up", took)
	}
}

// attachForMetricsAndStatus is attach, but it also answers MetricsQuery and
// StatusQuery (service health, FR: service health in issues), which share
// the stream with QueryLogs but aren't shaped the same.
func attachForMetricsAndStatus(t *testing.T, in *instance, key, agentID string) *fakeCoordinator {
	t.Helper()
	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return in.lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+key)
	stream, err := seismov1.NewCoordinatorClient(cc).Attach(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Hello{Hello: hello()}}); err != nil {
		t.Fatal(err)
	}

	f := &fakeCoordinator{stream: stream, ended: make(chan error, 1)}
	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				f.ended <- err
				return
			}
			switch {
			case req.GetQueryMetrics() != nil:
				q := req.GetQueryMetrics()
				_ = stream.Send(&seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Metrics{Metrics: &seismov1.AggregateMetrics{
					SchemaVersion: 1,
					QueryId:       q.GetQueryId(),
					Agents: []*seismov1.AgentMetrics{{
						AgentId: agentID, Status: seismov1.AgentStatus_AGENT_STATUS_OK,
						Services: []*seismov1.ServiceMetrics{{Service: "api", Lines: 100, ErrorLines: 3}},
					}},
				}}})
			case req.GetQueryStatus() != nil:
				q := req.GetQueryStatus()
				_ = stream.Send(&seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Status{Status: &seismov1.AggregateStatus{
					SchemaVersion: 1,
					QueryId:       q.GetQueryId(),
					Agents: []*seismov1.AgentStatusReport{{
						AgentId: agentID, Status: seismov1.AgentStatus_AGENT_STATUS_OK,
						Services: []*seismov1.ServiceStatus{{Service: "api", Status: seismov1.WorkloadStatus_WORKLOAD_STATUS_COMPLETE}},
					}},
				}}})
			}
		}
	}()
	return f
}

func TestQueryMetricsAndStatusReachTheAttachedCoordinator(t *testing.T) {
	nc := natsConn(t)
	appID := uuid.New()
	in := newInstance(t, nc, fakeAuth{"mco_good": appID})
	client := NewClient(nc, 2*time.Second, Limits{MaxBytes: 256 << 10, MaxLinesPerWorkload: 100})

	if _, err := client.QueryMetrics(context.Background(), appID, time.Now().Add(-time.Minute), time.Now()); !errors.Is(err, ErrNotAttached) {
		t.Fatalf("QueryMetrics before attach: err = %v, want ErrNotAttached", err)
	}
	if _, err := client.QueryStatus(context.Background(), appID); !errors.Is(err, ErrNotAttached) {
		t.Fatalf("QueryStatus before attach: err = %v, want ErrNotAttached", err)
	}

	attachForMetricsAndStatus(t, in, "mco_good", "agent-1")

	var metrics *seismov1.AggregateMetrics
	var err error
	for i := 0; i < 50; i++ {
		metrics, err = client.QueryMetrics(context.Background(), appID, time.Now().Add(-time.Minute), time.Now())
		if err == nil && len(metrics.GetAgents()) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || len(metrics.GetAgents()) != 1 || metrics.GetAgents()[0].GetAgentId() != "agent-1" {
		t.Fatalf("QueryMetrics = %v, %v", metrics, err)
	}
	if got := metrics.GetAgents()[0].GetServices()[0]; got.GetLines() != 100 || got.GetErrorLines() != 3 {
		t.Errorf("service metrics = %+v", got)
	}

	status, err := client.QueryStatus(context.Background(), appID)
	if err != nil || len(status.GetAgents()) != 1 || status.GetAgents()[0].GetAgentId() != "agent-1" {
		t.Fatalf("QueryStatus = %v, %v", status, err)
	}
}
