package triage

import (
	"slices"
	"testing"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

func TestAnalyze(t *testing.T) {
	t.Run("nil report returns empty Suggestion safely", func(t *testing.T) {
		got := Analyze(nil)
		if got.Severity != "" {
			t.Errorf("expected empty Severity, got %q", got.Severity)
		}
		if got.SeverityReason != "" {
			t.Errorf("expected empty SeverityReason, got %q", got.SeverityReason)
		}
		if len(got.Labels) != 0 {
			t.Errorf("expected empty Labels, got %v", got.Labels)
		}
		if got.Screen != "" {
			t.Errorf("expected empty Screen, got %q", got.Screen)
		}
	})

	t.Run("fatal crash with multiple breadcrumbs", func(t *testing.T) {
		r := &ingest.Report{
			Platform: "ios",
			Trigger:  "crash",
			Crash: &ingest.Crash{
				Type:       "EXC_BAD_ACCESS",
				Reason:     "Attempted to dereference null pointer",
				StackTrace: []string{"0x0001 CartViewController.swift:42"},
			},
			Breadcrumbs: []ingest.Breadcrumb{
				{Kind: "navigation", Message: "HomeScreen"},
				{Kind: "tap", Message: "CartButton"},
				{Kind: "navigation", Message: "CheckoutScreen"},
				{Kind: "tap", Message: "SubmitOrder"},
			},
		}

		got := Analyze(r)
		if got.Severity != SeverityCritical {
			t.Errorf("expected severity %q, got %q", SeverityCritical, got.Severity)
		}
		if got.Screen != "CheckoutScreen" {
			t.Errorf("expected screen %q, got %q", "CheckoutScreen", got.Screen)
		}
		if !slices.Contains(got.Labels, "ios") {
			t.Errorf("expected labels to contain 'ios', got %v", got.Labels)
		}
		if !slices.Contains(got.Labels, "crash") {
			t.Errorf("expected labels to contain 'crash', got %v", got.Labels)
		}
	})

	t.Run("OOM crash on non_fatal", func(t *testing.T) {
		r := &ingest.Report{
			Platform: "android",
			Trigger:  "non_fatal",
			Crash: &ingest.Crash{
				Type:   "java.lang.OutOfMemoryError",
				Reason: "Failed to allocate 16MB bitmap",
			},
		}

		got := Analyze(r)
		if got.Severity != SeverityCritical {
			t.Errorf("expected severity %q, got %q", SeverityCritical, got.Severity)
		}
		if !slices.Contains(got.Labels, "oom") {
			t.Errorf("expected labels to contain 'oom', got %v", got.Labels)
		}
	})

	t.Run("non-fatal exception where UserInput.Category is crash without duplicates", func(t *testing.T) {
		r := &ingest.Report{
			Platform: "flutter",
			Trigger:  "non_fatal",
			UserInput: &ingest.UserInput{
				Category: "crash",
			},
			Crash: &ingest.Crash{
				Type:   "StateError",
				Reason: "Bad state: closed",
			},
		}

		got := Analyze(r)
		if got.Severity != SeverityHigh {
			t.Errorf("expected severity %q, got %q", SeverityHigh, got.Severity)
		}

		crashCount := 0
		for _, l := range got.Labels {
			if l == "crash" {
				crashCount++
			}
		}
		if crashCount != 1 {
			t.Errorf("expected 'crash' label to appear exactly once, got %d times in %v", crashCount, got.Labels)
		}
		if !slices.Contains(got.Labels, "flutter") {
			t.Errorf("expected labels to contain 'flutter', got %v", got.Labels)
		}
		if !slices.Contains(got.Labels, "non-fatal") {
			t.Errorf("expected labels to contain 'non-fatal', got %v", got.Labels)
		}
	})

	t.Run("503 network request without crash", func(t *testing.T) {
		status503 := 503
		r := &ingest.Report{
			Platform: "web",
			Trigger:  "manual",
			NetworkRequests: []ingest.NetworkRequest{
				{
					URL:        "https://api.example.com/checkout",
					Method:     "POST",
					StatusCode: &status503,
				},
			},
		}

		got := Analyze(r)
		if got.Severity != SeverityHigh {
			t.Errorf("expected severity %q, got %q", SeverityHigh, got.Severity)
		}
		if !slices.Contains(got.Labels, "network-error") {
			t.Errorf("expected labels to contain 'network-error', got %v", got.Labels)
		}
	})

	t.Run("error log without crash or 5xx", func(t *testing.T) {
		r := &ingest.Report{
			Platform: "android",
			Trigger:  "shake",
			Logs: []ingest.LogLine{
				{Level: "info", Message: "User loaded profile"},
				{Level: "error", Message: "Avatar image failed to decode"},
			},
		}

		got := Analyze(r)
		if got.Severity != SeverityMedium {
			t.Errorf("expected severity %q, got %q", SeverityMedium, got.Severity)
		}
	})

	t.Run("UI shake report with no errors", func(t *testing.T) {
		r := &ingest.Report{
			Platform: "ios",
			Trigger:  "shake",
			UserInput: &ingest.UserInput{
				Category:    "ui",
				Description: "Dark mode font is hard to read",
			},
			Logs: []ingest.LogLine{
				{Level: "info", Message: "Device motion shake detected"},
			},
		}

		got := Analyze(r)
		if got.Severity != SeverityLow {
			t.Errorf("expected severity %q, got %q", SeverityLow, got.Severity)
		}
		if !slices.Contains(got.Labels, "ios") {
			t.Errorf("expected labels to contain 'ios', got %v", got.Labels)
		}
		if !slices.Contains(got.Labels, "ui") {
			t.Errorf("expected labels to contain 'ui', got %v", got.Labels)
		}
	})
}
