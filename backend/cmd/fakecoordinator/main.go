// Command fakecoordinator is a development-only stand-in for an app's log
// coordinator. It attaches to the backend's Coordinator.Attach with a
// coordinator key and answers every QueryLogs with made-up logs from two
// services, so server logs in issues can be tried without agents.
//
//	FAKECOORDINATOR_BACKEND  backend gRPC address (default localhost:9443), plain gRPC
//	FAKECOORDINATOR_KEY      the app's mco_ key (required)
//	FAKECOORDINATOR_ID       coordinator_id sent in Hello (default fake-coordinator)
//
// It attaches again with backoff when the stream ends.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

func main() {
	addr := env("FAKECOORDINATOR_BACKEND", "localhost:9443")
	key := os.Getenv("FAKECOORDINATOR_KEY")
	id := env("FAKECOORDINATOR_ID", "fake-coordinator")
	if key == "" {
		log.Fatal("FAKECOORDINATOR_KEY is required")
	}

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}

	wait := time.Second
	for {
		err := run(cc, key, id)
		log.Printf("stream ended: %v; attaching again in %s", err, wait)
		time.Sleep(wait)
		wait = min(2*wait, 30*time.Second)
	}
}

func run(cc *grpc.ClientConn, key, id string) error {
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+key)
	stream, err := seismov1.NewCoordinatorClient(cc).Attach(ctx)
	if err != nil {
		return err
	}
	hello := &seismov1.Hello{ProtocolVersion: 1, CoordinatorId: id}
	if err := stream.Send(&seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Hello{Hello: hello}}); err != nil {
		return err
	}
	log.Printf("attached to %s as %s", cc.Target(), id)

	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		q := req.GetQueryLogs()
		if q == nil {
			continue
		}
		log.Printf("query %s for [%s, %s], budget %d bytes", q.GetQueryId(),
			q.GetWindow().GetFrom().AsTime().Format(time.RFC3339), q.GetWindow().GetTo().AsTime().Format(time.RFC3339),
			q.GetLimits().GetMaxBytesTotal())
		if err := stream.Send(&seismov1.CoordinatorEvent{Event: &seismov1.CoordinatorEvent_Result{Result: answer(q)}}); err != nil {
			return err
		}
	}
}

// answer makes up one agent watching two services, with lines spread over
// the query's window.
func answer(q *seismov1.QueryLogs) *seismov1.AggregateResult {
	from, to := q.GetWindow().GetFrom().AsTime(), q.GetWindow().GetTo().AsTime()
	at := func(f float64) *timestamppb.Timestamp {
		return timestamppb.New(from.Add(time.Duration(f * float64(to.Sub(from)))))
	}
	running := &seismov1.StringDiagnostic{Value: &seismov1.StringDiagnostic_Available{Available: "running"}}

	api := &seismov1.WorkloadResult{
		Workload: &seismov1.Workload{Id: "c0ffee01", Name: "api", ScopeLabel: "fake-host"},
		Status:   seismov1.WorkloadStatus_WORKLOAD_STATUS_COMPLETE,
		Coverage: seismov1.CoverageStatus_COVERAGE_STATUS_COMPLETE,
		Diagnostics: &seismov1.WorkloadDiagnostics{
			RuntimeState: running,
			RestartCount: &seismov1.UintDiagnostic{Value: &seismov1.UintDiagnostic_Available{Available: 0}},
			Image:        &seismov1.StringDiagnostic{Value: &seismov1.StringDiagnostic_Available{Available: "example/api:1.4.2"}},
		},
		Entries: []*seismov1.LogEntry{
			logLine(at(0.40), seismov1.LogStream_LOG_STREAM_STDOUT, "GET /v1/cart 200 12ms"),
			logLine(at(0.48), seismov1.LogStream_LOG_STREAM_STDOUT, "POST /v1/payments -> payments:8080"),
			logLine(at(0.50), seismov1.LogStream_LOG_STREAM_STDERR, "POST /v1/payments 502 upstream timeout after 5000ms"),
		},
	}
	payments := &seismov1.WorkloadResult{
		Workload: &seismov1.Workload{Id: "c0ffee02", Name: "payments", ScopeLabel: "fake-host"},
		Status:   seismov1.WorkloadStatus_WORKLOAD_STATUS_COMPLETE,
		Coverage: seismov1.CoverageStatus_COVERAGE_STATUS_COMPLETE,
		Diagnostics: &seismov1.WorkloadDiagnostics{
			RuntimeState: running,
			RestartCount: &seismov1.UintDiagnostic{Value: &seismov1.UintDiagnostic_Available{Available: 2}},
			OomKilled:    &seismov1.BoolDiagnostic{Value: &seismov1.BoolDiagnostic_Available{Available: true}},
			Image:        &seismov1.StringDiagnostic{Value: &seismov1.StringDiagnostic_Available{Available: "example/payments:2.0.0"}},
		},
		Entries: []*seismov1.LogEntry{
			logLine(at(0.45), seismov1.LogStream_LOG_STREAM_STDERR, "db pool exhausted (20/20), waiting"),
			logLine(at(0.49), seismov1.LogStream_LOG_STREAM_STDERR, "card_number=[REDACTED] charge failed: context deadline exceeded"),
		},
	}

	return &seismov1.AggregateResult{
		SchemaVersion:   1,
		QueryId:         q.GetQueryId(),
		RequestedWindow: q.GetWindow(),
		Agents: []*seismov1.AgentSection{{
			AgentId: "fake-agent-1",
			Source:  seismov1.AgentSource_AGENT_SOURCE_DOCKER,
			Status:  seismov1.AgentStatus_AGENT_STATUS_OK,
			Result: &seismov1.QueryResult{
				QueryId:         q.GetQueryId(),
				Status:          seismov1.QueryStatus_QUERY_STATUS_COMPLETE,
				RequestedWindow: q.GetWindow(),
				Workloads:       []*seismov1.WorkloadResult{api, payments},
			},
		}},
	}
}

func logLine(ts *timestamppb.Timestamp, stream seismov1.LogStream, msg string) *seismov1.LogEntry {
	return &seismov1.LogEntry{Timestamp: ts, Stream: stream, Message: msg, MatchKind: seismov1.MatchKind_MATCH_KIND_WINDOW}
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
