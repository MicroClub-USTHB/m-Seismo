package worker

import (
	"fmt"
	"strings"

	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// Limits on the server logs in an issue, which share the body with
// everything else.
const (
	maxServerLogBytes  = 30000
	maxServerLogLines  = 40
	serverLogTimestamp = "15:04:05.000"
)

// renderServerLogs writes the coordinator's answer: per agent and
// service, its health and its last lines around the report.
func renderServerLogs(b *strings.Builder, agg *seismov1.AggregateResult) {
	if agg == nil {
		return
	}
	b.WriteString("### Server logs\n\n")
	if len(agg.GetAgents()) == 0 {
		b.WriteString("_No log agent was connected at the time._\n\n")
		return
	}
	for _, w := range agg.GetWarnings() {
		fmt.Fprintf(b, "> %s: %s\n", enumName(w.GetCode().String(), "WARNING_CODE_"), text(w.GetDetail()))
	}
	if len(agg.GetWarnings()) > 0 {
		b.WriteString("\n")
	}

	start := b.Len()
	for _, a := range agg.GetAgents() {
		fmt.Fprintf(b, "**Agent `%s`** (%s): %s", a.GetAgentId(), enumName(a.GetSource().String(), "AGENT_SOURCE_"), enumName(a.GetStatus().String(), "AGENT_STATUS_"))
		if a.GetDetail() != "" {
			b.WriteString(", " + text(oneLine(a.GetDetail())))
		}
		b.WriteString("\n\n")

		for _, wl := range a.GetResult().GetWorkloads() {
			if b.Len()-start > maxServerLogBytes {
				b.WriteString("_More services were cut to keep the issue readable._\n\n")
				return
			}
			renderWorkload(b, wl)
		}
	}
}

func renderWorkload(b *strings.Builder, wl *seismov1.WorkloadResult) {
	name := wl.GetWorkload().GetName()
	if scope := wl.GetWorkload().GetScopeLabel(); scope != "" {
		name += " (" + scope + ")"
	}
	summary := fmt.Sprintf("%s: %s", cell(name), enumName(wl.GetStatus().String(), "WORKLOAD_STATUS_"))
	if h := health(wl.GetDiagnostics()); h != "" {
		summary += " · " + h
	}
	fmt.Fprintf(b, "<details><summary>%s</summary>\n\n", summary)

	if e := wl.GetError(); e != nil {
		fmt.Fprintf(b, "%s: %s\n\n", enumName(e.GetKind().String(), "SOURCE_ERROR_KIND_"), text(oneLine(e.GetDetail())))
	}
	entries := last(wl.GetEntries(), maxServerLogLines)
	if len(entries) > 0 {
		lines := make([]string, len(entries))
		for i, e := range entries {
			ts := "--:--:--.---"
			if e.GetTimestamp() != nil {
				ts = e.GetTimestamp().AsTime().UTC().Format(serverLogTimestamp)
			}
			lines[i] = fmt.Sprintf("%s %s %s", ts, enumName(e.GetStream().String(), "LOG_STREAM_"), e.GetMessage())
		}
		b.WriteString(code(strings.Join(lines, "\n")))
	} else {
		b.WriteString("_No lines in the window._\n")
	}
	hidden := wl.GetDroppedLineCount()
	if n := len(wl.GetEntries()) - len(entries); n > 0 {
		hidden += uint64(n)
	}
	if hidden > 0 {
		fmt.Fprintf(b, "\n_%d older lines not shown._\n", hidden)
	}
	b.WriteString("\n</details>\n\n")
}

// health summarises a workload's diagnostics, leaving out what is unknown.
func health(d *seismov1.WorkloadDiagnostics) string {
	var parts []string
	if s := d.GetRuntimeState().GetAvailable(); s != "" {
		parts = append(parts, s)
	}
	if n := d.GetRestartCount().GetAvailable(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d restarts", n))
	}
	if d.GetOomKilled().GetAvailable() {
		parts = append(parts, "OOM killed")
	}
	if img := d.GetImage().GetAvailable(); img != "" {
		parts = append(parts, "`"+strings.ReplaceAll(img, "`", "")+"`")
	}
	return strings.Join(parts, " · ")
}

// enumName turns "AGENT_STATUS_TIMEOUT" into "timeout".
func enumName(s, prefix string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(s, prefix), "_", " "))
}
