package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
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

type fakeFailures struct {
	recorded []db.MarkReportFailedParams
}

func (f *fakeFailures) MarkReportFailed(_ context.Context, arg db.MarkReportFailedParams) error {
	f.recorded = append(f.recorded, arg)
	return nil
}

func TestHandleSettlesMessages(t *testing.T) {
	reportID := uuid.New()
	data, _ := json.Marshal(queue.ReportAccepted{ID: reportID, AppID: uuid.New()})
	rl := &issue.RateLimitError{Tracker: "github", Wait: 42 * time.Second, Err: errors.New("403")}

	tests := []struct {
		name        string
		data        []byte
		delivered   uint64
		err         error
		want        string
		wantDelay   time.Duration
		wantFailure string
	}{
		{name: "done", err: nil, want: "ack"},
		{name: "permanent", err: permanent(errors.New("gone")), want: "term", wantFailure: "gone"},
		{name: "rate limited", err: rl, want: "nak", wantDelay: 42 * time.Second},
		{name: "first failure", delivered: 1, err: errors.New("down"), want: "nak", wantDelay: backoff[0]},
		{name: "later failure", delivered: 9, err: errors.New("down"), want: "nak", wantDelay: backoff[len(backoff)-1]},
		{name: "last attempt", delivered: 10, err: errors.New("down"), want: "term", wantFailure: "down"},
		{name: "unreadable message", data: []byte("{"), want: "term"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := &fakeFailures{}
			w := New(nil, procFunc(func(context.Context, uuid.UUID) error { return tt.err }), failures, 1, 10)
			msg := &fakeMsg{data: data, delivered: max(tt.delivered, 1)}
			if tt.data != nil {
				msg.data = tt.data
			}
			w.handle(context.Background(), msg)
			if msg.settled != tt.want || msg.delay != tt.wantDelay {
				t.Errorf("settled %s after %s, want %s after %s", msg.settled, msg.delay, tt.want, tt.wantDelay)
			}
			if tt.wantFailure == "" {
				if len(failures.recorded) != 0 {
					t.Errorf("recorded unexpected failures: %+v", failures.recorded)
				}
			} else if len(failures.recorded) != 1 || failures.recorded[0].ID != reportID || failures.recorded[0].DeliveryError != tt.wantFailure {
				t.Errorf("recorded failures = %+v, want ID=%s DeliveryError=%q", failures.recorded, reportID, tt.wantFailure)
			}
		})
	}
}

func TestHandleLeavesMessageOnShutdown(t *testing.T) {
	data, _ := json.Marshal(queue.ReportAccepted{ID: uuid.New()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failures := &fakeFailures{}
	w := New(nil, procFunc(func(ctx context.Context, _ uuid.UUID) error { return ctx.Err() }), failures, 1, 10)
	msg := &fakeMsg{data: data, delivered: 10}
	w.handle(ctx, msg)
	if msg.settled != "nak" || msg.delay != 0 {
		t.Errorf("settled %s after %s, want an immediate nak", msg.settled, msg.delay)
	}
	if len(failures.recorded) != 0 {
		t.Errorf("recorded failure on shutdown: %+v", failures.recorded)
	}
}
