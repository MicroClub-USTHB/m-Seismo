package worker

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

func TestRenderServerLogs(t *testing.T) {
	at := time.Date(2026, 10, 21, 14, 3, 9, 870_000_000, time.UTC)
	agg := &seismov1.AggregateResult{
		Agents: []*seismov1.AgentSection{
			{
				AgentId: "prod-1",
				Source:  seismov1.AgentSource_AGENT_SOURCE_DOCKER,
				Status:  seismov1.AgentStatus_AGENT_STATUS_OK,
				Result: &seismov1.QueryResult{Workloads: []*seismov1.WorkloadResult{{
					Workload: &seismov1.Workload{Name: "payments", ScopeLabel: "host-a"},
					Status:   seismov1.WorkloadStatus_WORKLOAD_STATUS_COMPLETE,
					Diagnostics: &seismov1.WorkloadDiagnostics{
						RuntimeState: &seismov1.StringDiagnostic{Value: &seismov1.StringDiagnostic_Available{Available: "running"}},
						RestartCount: &seismov1.UintDiagnostic{Value: &seismov1.UintDiagnostic_Available{Available: 3}},
						OomKilled:    &seismov1.BoolDiagnostic{Value: &seismov1.BoolDiagnostic_Available{Available: true}},
					},
					Entries: []*seismov1.LogEntry{{
						Timestamp: timestamppb.New(at),
						Stream:    seismov1.LogStream_LOG_STREAM_STDERR,
						Message:   "POST /v1/payments 502 upstream timeout",
					}},
					DroppedLineCount: 12,
				}}},
			},
			{AgentId: "prod-2", Status: seismov1.AgentStatus_AGENT_STATUS_TIMEOUT, Detail: "no answer before the deadline"},
		},
	}
	body := render(&ingest.Report{}, reportInfo{ServerLogs: agg}).Body

	for _, want := range []string{
		"### Server logs",
		"**Agent `prod-1`** (docker): ok",
		"<summary>payments (host-a): complete · running · 3 restarts · OOM killed</summary>",
		"14:03:09.870 stderr POST /v1/payments 502 upstream timeout",
		"_12 older lines not shown._",
		"**Agent `prod-2`** (unspecified): timeout, no answer before the deadline",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
}

func TestRenderServerLogsNoAgent(t *testing.T) {
	body := render(&ingest.Report{}, reportInfo{ServerLogs: &seismov1.AggregateResult{}}).Body
	if !strings.Contains(body, "No log agent was connected") {
		t.Errorf("body:\n%s", body)
	}
	if body := render(&ingest.Report{}, reportInfo{}).Body; strings.Contains(body, "Server logs") {
		t.Errorf("server logs section without an answer:\n%s", body)
	}
}

func TestRenderServerLogsStaysBounded(t *testing.T) {
	line := strings.Repeat("x", 500)
	var workloads []*seismov1.WorkloadResult
	for i := 0; i < 200; i++ {
		var entries []*seismov1.LogEntry
		for j := 0; j < 100; j++ {
			entries = append(entries, &seismov1.LogEntry{Message: line})
		}
		workloads = append(workloads, &seismov1.WorkloadResult{Workload: &seismov1.Workload{Name: "svc"}, Entries: entries})
	}
	agg := &seismov1.AggregateResult{Agents: []*seismov1.AgentSection{{AgentId: "a", Result: &seismov1.QueryResult{Workloads: workloads}}}}
	body := render(&ingest.Report{}, reportInfo{ServerLogs: agg}).Body
	if len(body) > maxBody {
		t.Errorf("body is %d bytes, over %d", len(body), maxBody)
	}
	if !strings.Contains(body, "More services were cut") {
		t.Error("cut services are not mentioned")
	}
}
