// Package worker is the pipeline behind the ingestion API: it reads the
// reports announced on the queue and opens their issues.
//
// For each message the worker loads the report, opens its issue in the
// default repository of every project its app is linked to, and only then
// acknowledges the message. A failure leaves the message on the queue to be
// tried again later, so a report is not lost when a tracker is down or a
// worker stops halfway.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
)

const (
	// ackWait is how long JetStream waits for news of a message before
	// handing it to another worker. A worker says it is still working
	// every heartbeat, so only a worker that stopped loses its messages.
	ackWait   = time.Minute
	heartbeat = 20 * time.Second
	// processTimeout bounds the work on one report. Opening an issue can
	// wait out a tracker's rate limit, so it is far above a normal run.
	processTimeout = 5 * time.Minute
)

// backoff is the wait before each new attempt after a temporary failure;
// the last value repeats.
var backoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// ReportProcessor is what the worker runs for each report. Implemented by
// Processor.
type ReportProcessor interface {
	Process(ctx context.Context, id uuid.UUID) error
}

// Worker reads accepted reports from the queue and hands each one to the
// processor, a few at a time.
type Worker struct {
	js          jetstream.JetStream
	proc        ReportProcessor
	concurrency int
	maxAttempts int

	iter   jetstream.MessagesContext
	cancel context.CancelFunc
	done   chan struct{}
}

func New(js jetstream.JetStream, proc ReportProcessor, concurrency, maxAttempts int) *Worker {
	return &Worker{js: js, proc: proc, concurrency: concurrency, maxAttempts: maxAttempts}
}

// Start creates the consumer and starts reading messages in the
// background.
func (w *Worker) Start(ctx context.Context) error {
	cons, err := queue.ReportConsumer(ctx, w.js, ackWait, w.concurrency)
	if err != nil {
		return err
	}
	iter, err := cons.Messages(jetstream.PullMaxMessages(w.concurrency))
	if err != nil {
		return fmt.Errorf("worker: read %s: %w", queue.ConsumerReportWorker, err)
	}
	w.iter = iter

	base, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go w.run(base)
	slog.Info("worker started", "consumer", queue.ConsumerReportWorker, "concurrency", w.concurrency)
	return nil
}

// Stop stops reading messages and waits for the reports in progress. When
// ctx ends first, their work is cancelled and their messages are left for
// the next worker.
func (w *Worker) Stop(ctx context.Context) error {
	if w.iter == nil {
		return nil
	}
	w.iter.Stop()
	select {
	case <-w.done:
	case <-ctx.Done():
		w.cancel()
		<-w.done
	}
	w.cancel()
	return nil
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.done)
	var wg sync.WaitGroup
	defer wg.Wait()

	slots := make(chan struct{}, w.concurrency)
	for {
		msg, err := w.iter.Next()
		if errors.Is(err, jetstream.ErrMsgIteratorClosed) {
			return
		}
		if err != nil {
			// Usually NATS being unreachable; the iterator reconnects.
			slog.Warn("worker: read message", "err", err)
			time.Sleep(time.Second)
			continue
		}
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			w.handle(ctx, msg)
		}()
	}
}

// handle processes one message and settles it: acknowledged when done or
// not worth trying again, and otherwise handed back to the queue to come
// back after a wait.
func (w *Worker) handle(ctx context.Context, msg jetstream.Msg) {
	var m queue.ReportAccepted
	if err := json.Unmarshal(msg.Data(), &m); err != nil {
		slog.Error("worker: dropping unreadable message", "err", err)
		settle(msg.Term())
		return
	}
	attempt := 1
	if meta, err := msg.Metadata(); err == nil && meta.NumDelivered <= math.MaxInt32 {
		attempt = int(meta.NumDelivered)
	}
	log := slog.With("report", m.ID, "app", m.AppID, "attempt", attempt)

	ctx, cancel := context.WithTimeout(ctx, processTimeout)
	defer cancel()
	stop := keepAlive(ctx, msg)
	err := w.proc.Process(ctx, m.ID)
	stop()

	var perm *permanentError
	var rl *issue.RateLimitError
	switch {
	case err == nil:
		settle(msg.Ack())
	case errors.As(err, &perm):
		log.Warn("worker: report dropped", "err", err)
		settle(msg.Term())
	case errors.Is(err, context.Canceled) && ctx.Err() != nil:
		// Shutting down: the next worker takes it right away.
		settle(msg.Nak())
	case attempt >= w.maxAttempts:
		log.Error("worker: giving up on report", "err", err)
		settle(msg.Term())
	case errors.As(err, &rl):
		log.Info("worker: rate limited, trying again later", "wait", rl.Wait)
		settle(msg.NakWithDelay(rl.Wait))
	default:
		delay := backoff[min(attempt, len(backoff))-1]
		log.Warn("worker: report failed, trying again later", "wait", delay, "err", err)
		settle(msg.NakWithDelay(delay))
	}
}

// keepAlive tells JetStream every heartbeat that msg is still being worked
// on, until the returned function is called.
func keepAlive(ctx context.Context, msg jetstream.Msg) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				settle(msg.InProgress())
			}
		}
	}()
	return func() { cancel(); <-done }
}

// settle logs a failed acknowledgement. The message then comes back after
// ackWait, which processing tolerates.
func settle(err error) {
	if err != nil {
		slog.Warn("worker: acknowledge message", "err", err)
	}
}
