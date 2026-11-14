package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// relayMargin is how long past a query's deadline the relay keeps waiting,
// so the coordinator's answer at the deadline still makes it back.
const relayMargin = 500 * time.Millisecond

var (
	errReplaced = status.Error(codes.Aborted, "replaced by a newer Attach for this app")
	errShutdown = status.Error(codes.Unavailable, "backend instance is shutting down, attach again")
)

// announcement says that a coordinator attached for an app.
type announcement struct {
	StreamID uuid.UUID `json:"stream_id"`
	Since    time.Time `json:"since"`
}

// Registry keeps the Attach streams of this instance and relays queries to
// them from NATS.
type Registry struct {
	nc *nats.Conn

	mu       sync.Mutex
	attached map[uuid.UUID]*entry
	sub      *nats.Subscription
}

type entry struct {
	c    *conn
	subs []*nats.Subscription
}

func (e *entry) unsubscribe() {
	for _, sub := range e.subs {
		_ = sub.Unsubscribe()
	}
}

func NewRegistry(nc *nats.Conn) *Registry {
	return &Registry{nc: nc, attached: map[uuid.UUID]*entry{}}
}

// Start listens for attachments announced by other instances.
func (r *Registry) Start() error {
	sub, err := r.nc.Subscribe(attachedWildcard, r.onAnnouncement)
	if err != nil {
		return fmt.Errorf("coordinator: subscribe %s: %w", attachedWildcard, err)
	}
	r.sub = sub
	return nil
}

// Stop stops listening and closes every stream of this instance.
func (r *Registry) Stop() {
	if r.sub != nil {
		_ = r.sub.Unsubscribe()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for app, e := range r.attached {
		e.unsubscribe()
		e.c.evict(errShutdown)
		delete(r.attached, app)
	}
}

// add makes c the app's stream: an older one on this instance is closed,
// queries for the app are relayed to c, and other instances are told to
// close theirs.
func (r *Registry) add(c *conn) error {
	subs, err := subscribeQueries(r.nc, c, r)
	if err != nil {
		return err
	}

	r.mu.Lock()
	if old, ok := r.attached[c.appID]; ok {
		old.unsubscribe()
		old.c.evict(errReplaced)
	}
	r.attached[c.appID] = &entry{c: c, subs: subs}
	r.mu.Unlock()

	data, _ := json.Marshal(announcement{StreamID: c.id, Since: c.since})
	if err := r.nc.Publish(attachedSubject(c.appID), data); err != nil {
		slog.Warn("coordinator: announce attach", "app", c.appID, "err", err)
	}
	return nil
}

// subscribeQueries subscribes to every query subject of c's app, relaying
// each kind to c. Callbacks of one subscription run one at a time; queries
// don't wait for each other.
func subscribeQueries(nc *nats.Conn, c *conn, r *Registry) ([]*nats.Subscription, error) {
	kinds := []struct {
		subject string
		relay   func(*conn, *nats.Msg)
	}{
		{querySubject(c.appID), r.relay},
		{metricsQuerySubject(c.appID), r.relayMetrics},
		{statusQuerySubject(c.appID), r.relayStatus},
	}
	subs := make([]*nats.Subscription, 0, len(kinds))
	for _, k := range kinds {
		relay := k.relay
		sub, err := nc.Subscribe(k.subject, func(msg *nats.Msg) { go relay(c, msg) })
		if err != nil {
			for _, s := range subs {
				_ = s.Unsubscribe()
			}
			return nil, fmt.Errorf("coordinator: subscribe %s: %w", k.subject, err)
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

// remove forgets c, unless a newer stream already took its place.
func (r *Registry) remove(c *conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.attached[c.appID]; ok && e.c == c {
		e.unsubscribe()
		delete(r.attached, c.appID)
	}
}

// onAnnouncement closes this instance's stream for the app when another
// instance got a newer one.
func (r *Registry) onAnnouncement(msg *nats.Msg) {
	appID, err := uuid.Parse(strings.TrimPrefix(msg.Subject, "seismo.coordinator.attached."))
	if err != nil {
		return
	}
	var a announcement
	if err := json.Unmarshal(msg.Data, &a); err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.attached[appID]
	if !ok || e.c.id == a.StreamID || !e.c.since.Before(a.Since) {
		return
	}
	e.unsubscribe()
	e.c.evict(errReplaced)
	delete(r.attached, appID)
}

// relay answers one query from NATS with the coordinator's answer.
func (r *Registry) relay(c *conn, msg *nats.Msg) {
	var q seismov1.QueryLogs
	if err := proto.Unmarshal(msg.Data, &q); err != nil {
		slog.Warn("coordinator: dropping unreadable query", "app", c.appID, "err", err)
		return
	}
	req := &seismov1.BackendRequest{Request: &seismov1.BackendRequest_QueryLogs{QueryLogs: &q}}
	r.answer(c, msg, q.GetQueryId(), q.GetDeadline().AsTime(), req)
}

// relayMetrics is relay for a MetricsQuery.
func (r *Registry) relayMetrics(c *conn, msg *nats.Msg) {
	var q seismov1.MetricsQuery
	if err := proto.Unmarshal(msg.Data, &q); err != nil {
		slog.Warn("coordinator: dropping unreadable metrics query", "app", c.appID, "err", err)
		return
	}
	req := &seismov1.BackendRequest{Request: &seismov1.BackendRequest_QueryMetrics{QueryMetrics: &q}}
	r.answer(c, msg, q.GetQueryId(), q.GetDeadline().AsTime(), req)
}

// relayStatus is relay for a StatusQuery.
func (r *Registry) relayStatus(c *conn, msg *nats.Msg) {
	var q seismov1.StatusQuery
	if err := proto.Unmarshal(msg.Data, &q); err != nil {
		slog.Warn("coordinator: dropping unreadable status query", "app", c.appID, "err", err)
		return
	}
	req := &seismov1.BackendRequest{Request: &seismov1.BackendRequest_QueryStatus{QueryStatus: &q}}
	r.answer(c, msg, q.GetQueryId(), q.GetDeadline().AsTime(), req)
}

// answer sends req on c's stream and replies to msg with the coordinator's
// answer, or a synthesized AggregateError when it couldn't get one.
func (r *Registry) answer(c *conn, msg *nats.Msg, queryID string, deadline time.Time, req *seismov1.BackendRequest) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline.Add(relayMargin))
	defer cancel()

	ev, err := c.query(ctx, queryID, req)
	if err != nil {
		kind := seismov1.SourceErrorKind_SOURCE_ERROR_KIND_SOURCE_UNREACHABLE
		if errors.Is(err, context.DeadlineExceeded) {
			kind = seismov1.SourceErrorKind_SOURCE_ERROR_KIND_TIMEOUT
		}
		ev = errorEvent(queryID, kind, err.Error())
	}

	data, err := proto.Marshal(ev)
	if err == nil && int64(len(data)) > r.nc.MaxPayload() {
		err = fmt.Errorf("answer is %d bytes, over the %d bytes NATS carries", len(data), r.nc.MaxPayload())
	}
	if err != nil {
		data, _ = proto.Marshal(errorEvent(queryID, seismov1.SourceErrorKind_SOURCE_ERROR_KIND_INVALID_RESPONSE, err.Error()))
	}
	if err := msg.Respond(data); err != nil {
		slog.Warn("coordinator: answer query", "app", c.appID, "query", queryID, "err", err)
	}
}

func errorEvent(queryID string, kind seismov1.SourceErrorKind, detail string) *seismov1.CoordinatorEvent {
	return &seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Error{Error: &seismov1.AggregateError{
		QueryId: queryID,
		Error:   &seismov1.SourceError{Kind: kind, Detail: detail},
	}}}
}
