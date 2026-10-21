package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
)

// fakeMsg records how the worker settled it.
type fakeMsg struct {
	jetstream.Msg
	data      []byte
	delivered uint64
	settled   string
	delay     time.Duration
}

func (m *fakeMsg) Data() []byte { return m.data }
func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: m.delivered}, nil
}
func (m *fakeMsg) Ack() error        { m.settled = "ack"; return nil }
func (m *fakeMsg) Nak() error        { m.settled = "nak"; return nil }
func (m *fakeMsg) Term() error       { m.settled = "term"; return nil }
func (m *fakeMsg) InProgress() error { return nil }
func (m *fakeMsg) NakWithDelay(d time.Duration) error {
	m.settled, m.delay = "nak", d
	return nil
}

type procFunc func(ctx context.Context, id uuid.UUID) error

func (f procFunc) Process(ctx context.Context, id uuid.UUID) error { return f(ctx, id) }

func TestHandleSettlesMessages(t *testing.T) {
	data, _ := json.Marshal(queue.ReportAccepted{ID: uuid.New(), AppID: uuid.New()})
	rl := &issue.RateLimitError{Tracker: "github", Wait: 42 * time.Second, Err: errors.New("403")}

	tests := []struct {
		name      string
		data      []byte
		delivered uint64
		err       error
		want      string
		wantDelay time.Duration
	}{
		{name: "done", err: nil, want: "ack"},
		{name: "permanent", err: permanent(errors.New("gone")), want: "term"},
		{name: "rate limited", err: rl, want: "nak", wantDelay: 42 * time.Second},
		{name: "first failure", delivered: 1, err: errors.New("down"), want: "nak", wantDelay: backoff[0]},
		{name: "later failure", delivered: 9, err: errors.New("down"), want: "nak", wantDelay: backoff[len(backoff)-1]},
		{name: "last attempt", delivered: 10, err: errors.New("down"), want: "term"},
		{name: "unreadable message", data: []byte("{"), want: "term"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := New(nil, procFunc(func(context.Context, uuid.UUID) error { return tt.err }), 1, 10)
			msg := &fakeMsg{data: data, delivered: max(tt.delivered, 1)}
			if tt.data != nil {
				msg.data = tt.data
			}
			w.handle(context.Background(), msg)
			if msg.settled != tt.want || msg.delay != tt.wantDelay {
				t.Errorf("settled %s after %s, want %s after %s", msg.settled, msg.delay, tt.want, tt.wantDelay)
			}
		})
	}
}

func TestHandleLeavesMessageOnShutdown(t *testing.T) {
	data, _ := json.Marshal(queue.ReportAccepted{ID: uuid.New()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := New(nil, procFunc(func(ctx context.Context, _ uuid.UUID) error { return ctx.Err() }), 1, 10)
	msg := &fakeMsg{data: data, delivered: 10}
	w.handle(ctx, msg)
	if msg.settled != "nak" || msg.delay != 0 {
		t.Errorf("settled %s after %s, want an immediate nak", msg.settled, msg.delay)
	}
}
