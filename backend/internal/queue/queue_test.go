package queue

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// This test needs a NATS server with JetStream. It is skipped unless
// SEISMO_TEST_NATS_URL is set, e.g. nats://localhost:4222. Use a server no
// backend is running against: its worker's consumer would take the
// messages, and the test's own consumer can't be created next to it.
func TestReportAccepted(t *testing.T) {
	url := os.Getenv("SEISMO_TEST_NATS_URL")
	if url == "" {
		t.Skip("SEISMO_TEST_NATS_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := NewJetStream(ctx, js)
	if err != nil {
		t.Fatal(err)
	}
	// Creating the stream again must not fail (every instance does it).
	if _, err := NewJetStream(ctx, js); err != nil {
		t.Fatalf("second NewJetStream: %v", err)
	}

	stream, _ := js.Stream(ctx, StreamReports)
	before, _ := stream.Info(ctx)

	msg := ReportAccepted{ID: uuid.New(), AppID: uuid.New()}
	for i := 0; i < 3; i++ {
		if err := pub.ReportAccepted(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}

	after, _ := stream.Info(ctx)
	if got := after.State.Msgs - before.State.Msgs; got != 1 {
		t.Fatalf("stream gained %d messages for one report published 3 times, want 1", got)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "test-" + msg.ID.String(),
		FilterSubject: SubjectReportAccepted,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A work-queue stream accepts one consumer per subject, so a leftover
	// consumer would break the next run and any real worker.
	t.Cleanup(func() {
		if err := stream.DeleteConsumer(context.Background(), cons.CachedInfo().Name); err != nil {
			t.Errorf("delete test consumer: %v", err)
		}
	})
	batch, err := cons.Fetch(100, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for m := range batch.Messages() {
		var got ReportAccepted
		if json.Unmarshal(m.Data(), &got) == nil && got == msg {
			found = true
		}
		m.Ack()
	}
	if !found {
		t.Error("published message not received")
	}
}
