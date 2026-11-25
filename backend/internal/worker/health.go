package worker

import (
	"fmt"
	"strings"

	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// renderHealth renders the "Service health" table, nothing when rows is
// empty.
func renderHealth(b *strings.Builder, rows []healthRow) {
	if len(rows) == 0 {
		return
	}
	b.WriteString("### Service health\n\n")
	b.WriteString("| Agent | Service | Error rate | Restarts | CPU | Memory | Image |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	dash := func(s string) string {
		if s == "" {
			return "—"
		}
		return cell(s)
	}
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n",
			cell(r.Agent), cell(r.Service), dash(r.ErrorRate), dash(r.Restarts), dash(r.CPU), dash(r.Memory), dash(r.Image))
	}
	b.WriteString("\n")
}

// healthRow is one service's health snapshot for the issue's "Service
// health" table: error rate, restarts, configured CPU and memory, and
// deployed version. A field is empty when the agent didn't answer or the
// diagnostic is unavailable.
type healthRow struct {
	Agent     string
	Service   string
	ErrorRate string
	Restarts  string
	CPU       string
	Memory    string
	Image     string
}

// mergeHealth combines a MetricsQuery answer (error rate) and a
// StatusQuery answer (restarts, CPU, memory, image) into one row per
// (agent, service); either may be nil.
func mergeHealth(metrics *seismov1.AggregateMetrics, status *seismov1.AggregateStatus) []healthRow {
	type key struct{ agent, service string }
	rows := map[key]*healthRow{}
	var order []key
	row := func(agent, service string) *healthRow {
		k := key{agent, service}
		if r, ok := rows[k]; ok {
			return r
		}
		r := &healthRow{Agent: agent, Service: service}
		rows[k] = r
		order = append(order, k)
		return r
	}

	for _, a := range metrics.GetAgents() {
		for _, s := range a.GetServices() {
			if s.GetError() != nil {
				continue
			}
			row(a.GetAgentId(), s.GetService()).ErrorRate = errorRate(s.GetLines(), s.GetErrorLines())
		}
	}
	for _, a := range status.GetAgents() {
		for _, s := range a.GetServices() {
			if s.GetError() != nil {
				continue
			}
			r := row(a.GetAgentId(), s.GetService())
			d := s.GetDiagnostics()
			r.Restarts = uintString(d.GetRestartCount())
			r.CPU = nanocpuString(d.GetConfiguredCpuNanocpus())
			r.Memory = byteString(d.GetConfiguredMemoryBytes())
			r.Image = stringValue(d.GetImage())
		}
	}

	out := make([]healthRow, len(order))
	for i, k := range order {
		out[i] = *rows[k]
	}
	return out
}

// errorRate is "" when there were no lines to compute a rate from.
func errorRate(lines, errorLines uint64) string {
	if lines == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d (%.1f%%)", errorLines, lines, float64(errorLines)/float64(lines)*100)
}

func stringValue(d *seismov1.StringDiagnostic) string {
	if v, ok := d.GetValue().(*seismov1.StringDiagnostic_Available); ok {
		return v.Available
	}
	return ""
}

func uintString(d *seismov1.UintDiagnostic) string {
	if v, ok := d.GetValue().(*seismov1.UintDiagnostic_Available); ok {
		return fmt.Sprint(v.Available)
	}
	return ""
}

// nanocpuString renders a configured CPU limit in nanocpus as millicores
// (Kubernetes' usual unit), e.g. 500000000 -> "500m".
func nanocpuString(d *seismov1.UintDiagnostic) string {
	v, ok := d.GetValue().(*seismov1.UintDiagnostic_Available)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%dm", v.Available/1_000_000)
}

// byteString renders a byte count in the largest binary unit it divides
// into without going below 1.
func byteString(d *seismov1.UintDiagnostic) string {
	v, ok := d.GetValue().(*seismov1.UintDiagnostic_Available)
	if !ok {
		return ""
	}
	n := v.Available
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for n/div >= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
