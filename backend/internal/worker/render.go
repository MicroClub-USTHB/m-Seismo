package worker

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

// Limits on what goes into an issue. GitHub refuses titles over 256
// characters and bodies over 65536.
const (
	maxTitle       = 256
	maxBody        = 60000
	maxSteps       = 30
	maxStackFrames = 100
	maxLogLines    = 50
	maxRequests    = 20
)

// reportInfo is what the issue says about the stored report, besides its
// payload.
type reportInfo struct {
	ID             uuid.UUID
	InstallationID uuid.UUID
	Screenshot     bool
	// ServerLogs is the coordinator's answer, nil when there is none.
	ServerLogs *seismov1.AggregateResult
}

// render turns a report into the draft of its issue.
func render(r *ingest.Report, info reportInfo) issue.Draft {
	var b strings.Builder

	if in := r.UserInput; in != nil {
		if in.Description != "" {
			b.WriteString(text(in.Description))
			b.WriteString("\n\n")
		}
		var tags []string
		if in.Category != "" {
			tags = append(tags, "**Category:** "+in.Category)
		}
		if in.Severity != "" {
			tags = append(tags, "**Severity:** "+in.Severity)
		}
		if len(tags) > 0 {
			b.WriteString(strings.Join(tags, " · "))
			b.WriteString("\n\n")
		}
	}

	if steps := last(r.Breadcrumbs, maxSteps); len(steps) > 0 {
		b.WriteString("### Steps to reproduce\n\n")
		for i, s := range steps {
			fmt.Fprintf(&b, "%d. %s _(%s)_\n", i+1, text(s.Message), s.Kind)
		}
		b.WriteString("\n")
	}

	if c := r.Crash; c != nil {
		b.WriteString("### Crash\n\n")
		fmt.Fprintf(&b, "**%s**", text(c.Type))
		if c.Reason != "" {
			b.WriteString(": " + text(c.Reason))
		}
		b.WriteString("\n\n")
		frames := c.StackTrace
		if len(frames) > maxStackFrames {
			frames = append(frames[:maxStackFrames:maxStackFrames], fmt.Sprintf("... %d more frames", len(c.StackTrace)-maxStackFrames))
		}
		b.WriteString(code(strings.Join(frames, "\n")))
		b.WriteString("\n")
	}

	b.WriteString("### Environment\n\n")
	b.WriteString("| | |\n|---|---|\n")
	row := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "| %s | %s |\n", k, cell(v))
		}
	}
	if d := r.Device; d != nil {
		row("Device", d.Model)
		row("OS", strings.TrimSpace(d.OSName+" "+d.OSVersion))
		row("Locale", d.Locale)
		row("Network", d.NetworkType)
		row("Orientation", d.Orientation)
	}
	if rel := r.Release; rel != nil {
		row("Release", fmt.Sprintf("%s (%s)", rel.Version, rel.Build))
	}
	row("Platform", r.Platform)
	row("Environment", r.Environment)
	row("Mode", r.Mode)
	if s := r.SDK; s != nil {
		row("SDK", s.Name+" "+s.Version)
	}
	row("Trigger", r.Trigger)
	row("Occurred at", r.OccurredAt)
	for _, k := range sortedKeys(r.FeatureFlags) {
		row("Flag "+k, fmt.Sprint(r.FeatureFlags[k]))
	}
	for _, k := range sortedKeys(r.Experiments) {
		row("Experiment "+k, r.Experiments[k])
	}
	b.WriteString("\n")

	if logs := last(r.Logs, maxLogLines); len(logs) > 0 {
		fmt.Fprintf(&b, "### Logs (last %d)\n\n", len(logs))
		lines := make([]string, len(logs))
		for i, l := range logs {
			lines[i] = strings.TrimSpace(fmt.Sprintf("%s %s %s", l.Timestamp, strings.ToUpper(l.Level), l.Message))
		}
		b.WriteString(code(strings.Join(lines, "\n")))
		b.WriteString("\n")
	}

	// Bodies are never put in the issue, even when the SDK captured them.
	if reqs := last(r.NetworkRequests, maxRequests); len(reqs) > 0 {
		fmt.Fprintf(&b, "### Network requests (last %d)\n\n", len(reqs))
		lines := make([]string, len(reqs))
		for i, q := range reqs {
			status := "no response"
			if q.StatusCode != nil {
				status = fmt.Sprint(*q.StatusCode)
			}
			line := fmt.Sprintf("%s %s %s → %s", q.Timestamp, q.Method, q.URL, status)
			if q.DurationMS != nil {
				line += fmt.Sprintf(" (%d ms)", *q.DurationMS)
			}
			lines[i] = line
		}
		b.WriteString(code(strings.Join(lines, "\n")))
		b.WriteString("\n")
	}

	renderServerLogs(&b, info.ServerLogs)

	if info.Screenshot {
		b.WriteString("_A screenshot was sent with this report and is kept in m-Seismo._\n\n")
	}

	b.WriteString("---\n")
	fmt.Fprintf(&b, "<sub>Opened by m-Seismo · report `%s` · installation `%s`</sub>\n", info.ID, info.InstallationID)

	return issue.Draft{
		Title: truncate(title(r), maxTitle, "…"),
		Body:  truncate(b.String(), maxBody, "\n\n_(truncated)_"),
	}
}

// title is the reporter's title, or one made from the report.
func title(r *ingest.Report) string {
	if in := r.UserInput; in != nil && strings.TrimSpace(in.Title) != "" {
		return oneLine(in.Title)
	}
	if c := r.Crash; c != nil {
		t := "Crash: " + c.Type
		if c.Reason != "" {
			t += ": " + c.Reason
		}
		return oneLine(t)
	}
	t := "Report from " + r.Platform
	if r.Release != nil {
		t += " " + r.Release.Version
	}
	return oneLine(t + " (" + r.Trigger + ")")
}

// text makes reporter-written text safe to put in markdown: an "@" would
// notify a GitHub user, so it is followed by a zero-width space.
func text(s string) string {
	return strings.ReplaceAll(s, "@", "@\u200b")
}

// cell is text for a table cell, on one line and without column breaks.
func cell(s string) string {
	return strings.ReplaceAll(text(oneLine(s)), "|", `\|`)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// code fences s with more backticks than it contains in a row, so its
// content can't close the block.
func code(s string) string {
	fence, run := 3, 0
	for _, c := range s {
		if c == '`' {
			run++
			fence = max(fence, run+1)
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", fence)
	return f + "\n" + s + "\n" + f + "\n"
}

// truncate cuts s to at most n bytes, ending with more, without splitting
// a character.
func truncate(s string, n int, more string) string {
	if len(s) <= n {
		return s
	}
	cut := n - len(more)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + more
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func last[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
