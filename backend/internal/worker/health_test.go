package worker

import (
	"strings"
	"testing"

	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

func available(v uint64) *seismov1.UintDiagnostic {
	return &seismov1.UintDiagnostic{Value: &seismov1.UintDiagnostic_Available{Available: v}}
}

func unavailable(reason string) *seismov1.UintDiagnostic {
	return &seismov1.UintDiagnostic{Value: &seismov1.UintDiagnostic_UnavailableReason{UnavailableReason: reason}}
}

func TestMergeHealthCombinesMetricsAndStatus(t *testing.T) {
	metrics := &seismov1.AggregateMetrics{Agents: []*seismov1.AgentMetrics{{
		AgentId: "agent-1",
		Services: []*seismov1.ServiceMetrics{
			{Service: "api", Lines: 100, ErrorLines: 5},
			{Service: "no-lines", Lines: 0, ErrorLines: 0},
		},
	}}}
	status := &seismov1.AggregateStatus{Agents: []*seismov1.AgentStatusReport{{
		AgentId: "agent-1",
		Services: []*seismov1.ServiceStatus{{
			Service: "api",
			Diagnostics: &seismov1.WorkloadDiagnostics{
				RestartCount:          available(3),
				ConfiguredCpuNanocpus: available(500_000_000),
				ConfiguredMemoryBytes: available(512 << 20),
				Image:                 &seismov1.StringDiagnostic{Value: &seismov1.StringDiagnostic_Available{Available: "myapp:1.2.3"}},
			},
		}},
	}}}

	rows := mergeHealth(metrics, status)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2", rows)
	}
	api := rows[0]
	if api.Agent != "agent-1" || api.Service != "api" {
		t.Fatalf("row = %+v", api)
	}
	if api.ErrorRate != "5/100 (5.0%)" {
		t.Errorf("ErrorRate = %q", api.ErrorRate)
	}
	if api.Restarts != "3" {
		t.Errorf("Restarts = %q", api.Restarts)
	}
	if api.CPU != "500m" {
		t.Errorf("CPU = %q", api.CPU)
	}
	if api.Memory != "512.0 MiB" {
		t.Errorf("Memory = %q", api.Memory)
	}
	if api.Image != "myapp:1.2.3" {
		t.Errorf("Image = %q", api.Image)
	}

	noLines := rows[1]
	if noLines.ErrorRate != "" {
		t.Errorf("ErrorRate of a service with no lines = %q, want empty", noLines.ErrorRate)
	}
}

func TestMergeHealthLeavesUnavailableDiagnosticsEmpty(t *testing.T) {
	status := &seismov1.AggregateStatus{Agents: []*seismov1.AgentStatusReport{{
		AgentId: "agent-1",
		Services: []*seismov1.ServiceStatus{{
			Service: "api",
			Diagnostics: &seismov1.WorkloadDiagnostics{
				RestartCount: unavailable("permission denied"),
			},
		}},
	}}}
	rows := mergeHealth(nil, status)
	if len(rows) != 1 || rows[0].Restarts != "" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestMergeHealthSkipsServicesThatErrored(t *testing.T) {
	status := &seismov1.AggregateStatus{Agents: []*seismov1.AgentStatusReport{{
		AgentId: "agent-1",
		Services: []*seismov1.ServiceStatus{{
			Service: "api",
			Error:   &seismov1.SourceError{Detail: "unreachable"},
		}},
	}}}
	if rows := mergeHealth(nil, status); len(rows) != 0 {
		t.Errorf("rows = %+v, want none for a service that errored", rows)
	}
}

func TestRenderHealthEmptyIsNoop(t *testing.T) {
	var b strings.Builder
	renderHealth(&b, nil)
	if b.Len() != 0 {
		t.Errorf("rendered %q for no rows", b.String())
	}
}

func TestRenderHealthTable(t *testing.T) {
	var b strings.Builder
	renderHealth(&b, []healthRow{{Agent: "agent-1", Service: "api", ErrorRate: "1/10 (10.0%)", CPU: "500m"}})
	out := b.String()
	if !strings.Contains(out, "### Service health") {
		t.Errorf("missing heading: %q", out)
	}
	if !strings.Contains(out, "| agent-1 | api | 1/10 (10.0%) | — | 500m | — | — |") {
		t.Errorf("missing or malformed row: %q", out)
	}
}
