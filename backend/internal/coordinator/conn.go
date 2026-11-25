package coordinator

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// errDetached is returned for a query whose stream closed before the
// answer.
var errDetached = errors.New("coordinator: stream closed before the answer")

// sender is the part of the Attach stream a conn writes to.
type sender interface {
	Send(*seismov1.BackendRequest) error
}

// conn is one coordinator's Attach stream.
type conn struct {
	id            uuid.UUID
	appID         uuid.UUID
	coordinatorID string
	since         time.Time

	sendMu sync.Mutex
	stream sender

	mu      sync.Mutex
	pending map[string]chan *seismov1.CoordinatorEvent

	// evicted is closed when the backend ends the stream, with the status
	// in evictErr; done when the stream ended for any reason.
	evicted   chan struct{}
	evictErr  error
	evictOnce sync.Once
	done      chan struct{}
	doneOnce  sync.Once
}

func newConn(appID uuid.UUID, coordinatorID string, stream sender) *conn {
	return &conn{
		id:            uuid.New(),
		appID:         appID,
		coordinatorID: coordinatorID,
		since:         time.Now(),
		stream:        stream,
		pending:       map[string]chan *seismov1.CoordinatorEvent{},
		evicted:       make(chan struct{}),
		done:          make(chan struct{}),
	}
}

// query sends req on the stream and waits for the event answering queryID.
func (c *conn) query(ctx context.Context, queryID string, req *seismov1.BackendRequest) (*seismov1.CoordinatorEvent, error) {
	ch := make(chan *seismov1.CoordinatorEvent, 1)
	c.mu.Lock()
	c.pending[queryID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, queryID)
		c.mu.Unlock()
	}()

	c.sendMu.Lock()
	err := c.stream.Send(req)
	c.sendMu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case ev := <-ch:
		return ev, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errDetached
	}
}

// deliver hands an event from the coordinator to the query waiting for it.
// An answer nobody waits for any more (too late, or unknown) is dropped.
func (c *conn) deliver(ev *seismov1.CoordinatorEvent) {
	var queryID string
	switch e := ev.GetEvent().(type) {
	case *seismov1.CoordinatorEvent_Result:
		queryID = e.Result.GetQueryId()
	case *seismov1.CoordinatorEvent_Error:
		queryID = e.Error.GetQueryId()
	case *seismov1.CoordinatorEvent_Metrics:
		queryID = e.Metrics.GetQueryId()
	case *seismov1.CoordinatorEvent_Status:
		queryID = e.Status.GetQueryId()
	default:
		slog.Warn("coordinator: unexpected event after Hello", "app", c.appID, "coordinator", c.coordinatorID)
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[queryID]
	c.mu.Unlock()
	if !ok {
		slog.Debug("coordinator: dropping late or unknown answer", "app", c.appID, "query", queryID)
		return
	}
	select {
	case ch <- ev:
	default:
		// Already answered once; keep the first.
	}
}

// evict ends the stream with err, a gRPC status for the coordinator.
func (c *conn) evict(err error) {
	c.evictOnce.Do(func() {
		c.evictErr = err
		close(c.evicted)
	})
}

func (c *conn) close() { c.doneOnce.Do(func() { close(c.done) }) }
