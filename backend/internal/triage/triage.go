// Package triage provides heuristic and AI-assisted triage suggestions
// for incoming reports before issues are created.
package triage

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// Severities supported by triage inference.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

var fatalKeywords = []string{
	"fatal",
	"sigsegv",
	"sigabrt",
	"exc_bad_access",
	"outofmemory",
	"oom",
	"anr",
}

var oomKeywords = []string{
	"outofmemory",
	"out of memory",
	"oom",
	"jetsam",
}

// Suggestion contains heuristic or AI-generated triage recommendations for a
// report. All suggestions are machine-inferred and subject to human review.
type Suggestion struct {
	Severity       string     `json:"severity"`
	SeverityReason string     `json:"severity_reason"`
	Labels         []string   `json:"labels,omitempty"`
	Screen         string     `json:"screen,omitempty"`
	Owner          string     `json:"owner,omitempty"`
	OwnerReason    string     `json:"owner_reason,omitempty"`
	AISummary      *AISummary `json:"ai_summary,omitempty"`
}

// AISummary holds optional AI-generated title and probable root cause analysis.
type AISummary struct {
	Title         string `json:"title"`
	ProbableCause string `json:"probable_cause"`
}

// Analyze evaluates an ingested report and returns suggested severity, labels,
// and screen context based on heuristic rules.
func Analyze(r *ingest.Report) Suggestion {
	if r == nil {
		return Suggestion{}
	}

	sev, sevReason := suggestSeverity(r)
	labels := suggestLabels(r)
	screen := lastScreen(r)

	return Suggestion{
		Severity:       sev,
		SeverityReason: sevReason,
		Labels:         labels,
		Screen:         screen,
	}
}

func suggestSeverity(r *ingest.Report) (string, string) {
	if r.Trigger == "crash" {
		return SeverityCritical, "fatal crash detected"
	}
	if r.Crash != nil {
		combined := strings.ToLower(r.Crash.Type + " " + r.Crash.Reason)
		for _, kw := range fatalKeywords {
			if strings.Contains(combined, kw) {
				return SeverityCritical, fmt.Sprintf("fatal error detected (%s)", kw)
			}
		}
	}

	if r.Crash != nil || r.Trigger == "non_fatal" {
		return SeverityHigh, "non-fatal crash or exception detected"
	}
	if r.UserInput != nil && r.UserInput.Category == "crash" {
		return SeverityHigh, "user reported crash category"
	}
	for _, req := range r.NetworkRequests {
		if req.StatusCode != nil && *req.StatusCode >= 500 {
			return SeverityHigh, fmt.Sprintf("server error (%d) detected in network requests", *req.StatusCode)
		}
	}

	for _, l := range r.Logs {
		if strings.EqualFold(l.Level, "error") {
			return SeverityMedium, "error log detected"
		}
	}

	return SeverityLow, "no critical indicators detected"
}

func suggestLabels(r *ingest.Report) []string {
	var labels []string
	add := func(label string) {
		trimmed := strings.ToLower(strings.TrimSpace(label))
		if trimmed == "" {
			return
		}
		if !slices.Contains(labels, trimmed) {
			labels = append(labels, trimmed)
		}
	}

	if r.Platform != "" {
		add(r.Platform)
	}
	if r.Trigger == "crash" || r.Crash != nil {
		add("crash")
	}
	if r.Trigger == "non_fatal" {
		add("non-fatal")
	}
	if r.UserInput != nil && r.UserInput.Category != "" {
		add(r.UserInput.Category)
	}
	if r.Crash != nil {
		combined := strings.ToLower(r.Crash.Type + " " + r.Crash.Reason)
		for _, kw := range oomKeywords {
			if strings.Contains(combined, kw) {
				add("oom")
				break
			}
		}
	}
	for _, req := range r.NetworkRequests {
		if req.StatusCode != nil && *req.StatusCode >= 500 {
			add("network-error")
			break
		}
	}

	return labels
}

func lastScreen(r *ingest.Report) string {
	if r == nil {
		return ""
	}
	for i := len(r.Breadcrumbs) - 1; i >= 0; i-- {
		b := r.Breadcrumbs[i]
		if b.Kind == "navigation" {
			msg := strings.TrimSpace(b.Message)
			if msg != "" {
				return msg
			}
		}
	}
	return ""
}
