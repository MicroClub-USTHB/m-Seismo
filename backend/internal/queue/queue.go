// Package queue carries work between the ingestion API and the pipeline
// through NATS JetStream.
//
// Accepted reports are published on SubjectReportAccepted in the
// StreamReports stream. The stream is a work queue: each message is kept
// until a worker acknowledges it, so a report is not lost when no worker is
// running or one crashes halfway.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamReports         = "REPORTS"
	SubjectReportAccepted = "reports.accepted"

	// ConsumerReportWorker is the durable consumer the workers share. A
	// work-queue stream accepts one consumer per subject, and JetStream
	// spreads its messages across every worker reading from it.
	ConsumerReportWorker = "report-worker"

	// dedupWindow is how long JetStream remembers a message ID, so the
	// same report published twice in that window is stored once.
	dedupWindow = 10 * time.Minute
)

// ReportAccepted tells the pipeline that a report was stored.
type ReportAccepted struct {
	// ID is the backend's ID for the report (reports.id).
	ID    uuid.UUID `json:"id"`
	AppID uuid.UUID `json:"app_id"`
}

// Publisher announces accepted reports.
type Publisher interface {
	ReportAccepted(ctx context.Context, msg ReportAccepted) error
}

// JetStream is the Publisher backed by NATS JetStream.
type JetStream struct {
	js jetstream.JetStream
}

var _ Publisher = (*JetStream)(nil)

// EnsureStream creates or updates the reports stream. Every instance calls
// it at start-up, publisher or worker, so either can start first.
func EnsureStream(ctx context.Context, js jetstream.JetStream) (jetstream.Stream, error) {
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamReports,
		Subjects:   []string{"reports.>"},
		Retention:  jetstream.WorkQueuePolicy,
		Storage:    jetstream.FileStorage,
		Duplicates: dedupWindow,
	})
	if err != nil {
		return nil, fmt.Errorf("queue: create stream %s: %w", StreamReports, err)
	}
	return stream, nil
}

// NewJetStream creates or updates the reports stream and returns a
// publisher for it.
func NewJetStream(ctx context.Context, js jetstream.JetStream) (*JetStream, error) {
	if _, err := EnsureStream(ctx, js); err != nil {
		return nil, err
	}
	return &JetStream{js: js}, nil
}

// ReportConsumer creates or updates the workers' consumer of accepted
// reports. ackWait is how long a worker may hold a message without
// acknowledging it or saying it is still working on it before JetStream
// hands it to another worker; maxAckPending caps the messages handed out
// and not yet acknowledged.
func ReportConsumer(ctx context.Context, js jetstream.JetStream, ackWait time.Duration, maxAckPending int) (jetstream.Consumer, error) {
	stream, err := EnsureStream(ctx, js)
	if err != nil {
		return nil, err
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       ConsumerReportWorker,
		FilterSubject: SubjectReportAccepted,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       ackWait,
		MaxAckPending: maxAckPending,
	})
	if err != nil {
		return nil, fmt.Errorf("queue: create consumer %s: %w", ConsumerReportWorker, err)
	}
	return cons, nil
}

// ReportAccepted publishes msg and waits for JetStream to store it. The
// report ID is the message ID, so publishing the same report again within
// the dedup window is a no-op.
func (p *JetStream) ReportAccepted(ctx context.Context, msg ReportAccepted) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("queue: encode: %w", err)
	}
	if _, err := p.js.Publish(ctx, SubjectReportAccepted, data, jetstream.WithMsgID(msg.ID.String())); err != nil {
		return fmt.Errorf("queue: publish report %s: %w", msg.ID, err)
	}
	return nil
}
