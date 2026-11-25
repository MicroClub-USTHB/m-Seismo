package coordinator

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// ErrNotAttached is returned when no coordinator is attached for the app.
var ErrNotAttached = errors.New("coordinator: no coordinator attached for this app")

// payloadOverhead is kept free in a NATS message for everything around the
// logs themselves.
const payloadOverhead = 64 << 10

// Limits is the budget of one query.
type Limits struct {
	// MaxBytes is for the whole aggregate.
	MaxBytes            int
	MaxLinesPerWorkload int
}

// Client asks an app's coordinator for server logs, from any instance.
type Client struct {
	nc      *nats.Conn
	timeout time.Duration
	limits  Limits
}

func NewClient(nc *nats.Conn, timeout time.Duration, limits Limits) *Client {
	return &Client{nc: nc, timeout: timeout, limits: limits}
}

// QueryLogs returns the logs of every agent behind the app's coordinator in
// [from, to]. It waits at most the client's timeout, less if ctx ends
// sooner, and returns ErrNotAttached when no coordinator is attached.
func (c *Client) QueryLogs(ctx context.Context, appID uuid.UUID, from, to time.Time) (*seismov1.AggregateResult, error) {
	deadline := c.deadline(ctx)

	// The answer travels back in one NATS message.
	maxBytes := min(int64(c.limits.MaxBytes), c.nc.MaxPayload()-payloadOverhead)
	if maxBytes <= 0 {
		return nil, fmt.Errorf("coordinator: NATS max payload %d is too small", c.nc.MaxPayload())
	}

	q := &seismov1.QueryLogs{
		QueryId:  uuid.NewString(),
		Window:   &seismov1.TimeWindow{From: timestamppb.New(from), To: timestamppb.New(to)},
		Deadline: timestamppb.New(deadline),
		Limits: &seismov1.QueryLimits{
			MaxBytesTotal:       clampUint32(maxBytes),
			MaxLinesPerWorkload: clampUint32(int64(c.limits.MaxLinesPerWorkload)),
		},
	}
	ev, err := c.roundTrip(ctx, appID, querySubject(appID), deadline, q)
	if err != nil {
		return nil, err
	}
	switch e := ev.GetEvent().(type) {
	case *seismov1.CoordinatorEvent_Result:
		if e.Result.GetQueryId() != q.GetQueryId() {
			return nil, fmt.Errorf("coordinator: answer for query %q, want %q", e.Result.GetQueryId(), q.GetQueryId())
		}
		return e.Result, nil
	case *seismov1.CoordinatorEvent_Error:
		err := e.Error.GetError()
		return nil, fmt.Errorf("coordinator: %s: %s", err.GetKind(), err.GetDetail())
	default:
		return nil, errors.New("coordinator: empty answer")
	}
}

// QueryMetrics returns the log-based metrics (lines, error lines) of every
// agent behind the app's coordinator over [from, to]. Same waiting and
// ErrNotAttached rules as QueryLogs.
func (c *Client) QueryMetrics(ctx context.Context, appID uuid.UUID, from, to time.Time) (*seismov1.AggregateMetrics, error) {
	deadline := c.deadline(ctx)
	q := &seismov1.MetricsQuery{
		QueryId:  uuid.NewString(),
		Window:   &seismov1.TimeWindow{From: timestamppb.New(from), To: timestamppb.New(to)},
		Deadline: timestamppb.New(deadline),
	}
	ev, err := c.roundTrip(ctx, appID, metricsQuerySubject(appID), deadline, q)
	if err != nil {
		return nil, err
	}
	switch e := ev.GetEvent().(type) {
	case *seismov1.CoordinatorEvent_Metrics:
		if e.Metrics.GetQueryId() != q.GetQueryId() {
			return nil, fmt.Errorf("coordinator: answer for query %q, want %q", e.Metrics.GetQueryId(), q.GetQueryId())
		}
		return e.Metrics, nil
	case *seismov1.CoordinatorEvent_Error:
		err := e.Error.GetError()
		return nil, fmt.Errorf("coordinator: %s: %s", err.GetKind(), err.GetDetail())
	default:
		return nil, errors.New("coordinator: empty answer")
	}
}

// QueryStatus returns the container/pod status (restarts, OOM, image,
// configured CPU and memory) of every agent behind the app's coordinator.
// Same waiting and ErrNotAttached rules as QueryLogs.
func (c *Client) QueryStatus(ctx context.Context, appID uuid.UUID) (*seismov1.AggregateStatus, error) {
	deadline := c.deadline(ctx)
	q := &seismov1.StatusQuery{
		QueryId:  uuid.NewString(),
		Deadline: timestamppb.New(deadline),
	}
	ev, err := c.roundTrip(ctx, appID, statusQuerySubject(appID), deadline, q)
	if err != nil {
		return nil, err
	}
	switch e := ev.GetEvent().(type) {
	case *seismov1.CoordinatorEvent_Status:
		if e.Status.GetQueryId() != q.GetQueryId() {
			return nil, fmt.Errorf("coordinator: answer for query %q, want %q", e.Status.GetQueryId(), q.GetQueryId())
		}
		return e.Status, nil
	case *seismov1.CoordinatorEvent_Error:
		err := e.Error.GetError()
		return nil, fmt.Errorf("coordinator: %s: %s", err.GetKind(), err.GetDetail())
	default:
		return nil, errors.New("coordinator: empty answer")
	}
}

// deadline is the absolute time a query must be answered by: the client's
// timeout, or ctx's deadline when that comes first.
func (c *Client) deadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline.Add(relayMargin+time.Second)) {
		deadline = d.Add(-relayMargin - time.Second)
	}
	return deadline
}

// roundTrip marshals q, sends it on subject and decodes the coordinator's
// answer.
func (c *Client) roundTrip(ctx context.Context, appID uuid.UUID, subject string, deadline time.Time, q proto.Message) (*seismov1.CoordinatorEvent, error) {
	data, err := proto.Marshal(q)
	if err != nil {
		return nil, fmt.Errorf("coordinator: encode query: %w", err)
	}

	rctx, cancel := context.WithDeadline(ctx, deadline.Add(relayMargin+time.Second))
	defer cancel()
	msg, err := c.nc.RequestWithContext(rctx, subject, data)
	if errors.Is(err, nats.ErrNoResponders) {
		return nil, ErrNotAttached
	}
	if err != nil {
		return nil, fmt.Errorf("coordinator: query app %s: %w", appID, err)
	}

	var ev seismov1.CoordinatorEvent
	if err := proto.Unmarshal(msg.Data, &ev); err != nil {
		return nil, fmt.Errorf("coordinator: decode answer: %w", err)
	}
	return &ev, nil
}

func clampUint32(n int64) uint32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxUint32:
		return math.MaxUint32
	}
	return uint32(n)
}
