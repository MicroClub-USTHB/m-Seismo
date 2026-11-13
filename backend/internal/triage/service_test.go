package triage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

type mockSummarizer struct {
	mu      sync.Mutex
	summary *AISummary
	err     error
	delay   time.Duration
	called  int
	input   SummaryInput
}

func (m *mockSummarizer) Summarize(ctx context.Context, input SummaryInput) (*AISummary, error) {
	m.mu.Lock()
	m.called++
	m.input = input
	delay := m.delay
	summary := m.summary
	err := m.err
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return summary, err
}

func (m *mockSummarizer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

func sampleReport() *ingest.Report {
	return &ingest.Report{
		Platform: "ios",
		Trigger:  "crash",
		Crash: &ingest.Crash{
			Type:   "EXC_BAD_ACCESS",
			Reason: "Attempted to dereference null pointer in cart",
			StackTrace: []string{
				"0 AppCore Features/Checkout/CheckoutViewController.swift:42",
				"1 AppCore MainTabBarController.swift:10",
			},
		},
		Breadcrumbs: []ingest.Breadcrumb{
			{Kind: "navigation", Message: "HomeScreen"},
			{Kind: "tap", Message: "AddToCart"},
			{Kind: "navigation", Message: "CheckoutScreen"},
		},
		UserInput: &ingest.UserInput{
			Title:       "Crash on checkout",
			Description: "App died when I hit pay",
		},
	}
}

func TestService_HappyPath(t *testing.T) {
	fetcher := newMockFileFetcher()
	fetcher.setFile("org/app", ".github/CODEOWNERS", []byte("Features/Checkout/* @checkout-team\n"))
	codeownersProvider := NewCodeownersProvider(fetcher)

	ai := &mockSummarizer{
		summary: &AISummary{
			Title:         "[Crash] Null pointer in CheckoutViewController",
			ProbableCause: "Unchecked cart unwrapping after timeout",
		},
	}

	service := NewService(codeownersProvider, ai)
	s := service.TriageReport(context.Background(), sampleReport(), "org/app")

	if s.Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical, got %q", s.Severity)
	}
	if s.Owner != "@checkout-team" {
		t.Errorf("expected owner @checkout-team, got %q", s.Owner)
	}
	if !strings.Contains(s.OwnerReason, "Features/Checkout/*") {
		t.Errorf("expected OwnerReason to mention rule, got %q", s.OwnerReason)
	}
	if s.AISummary == nil {
		t.Fatal("expected non-nil AISummary")
	}
	if s.AISummary.Title != "[Crash] Null pointer in CheckoutViewController" {
		t.Errorf("unexpected AI title: %q", s.AISummary.Title)
	}
	if s.AISummary.ProbableCause != "Unchecked cart unwrapping after timeout" {
		t.Errorf("unexpected AI probable cause: %q", s.AISummary.ProbableCause)
	}
	if ai.callCount() != 1 {
		t.Errorf("expected 1 call to AI summarizer, got %d", ai.callCount())
	}
}

func TestService_PartialFailureTolerance(t *testing.T) {
	fetcher := newMockFileFetcher()
	fetcher.setFile("org/app", ".github/CODEOWNERS", []byte("Features/Checkout/* @checkout-team\n"))
	codeownersProvider := NewCodeownersProvider(fetcher)

	// Mock AI returning an error (e.g. rate limit, quota, outage)
	ai := &mockSummarizer{
		err: errors.New("gemini api quota exceeded (429)"),
	}

	service := NewService(codeownersProvider, ai)
	s := service.TriageReport(context.Background(), sampleReport(), "org/app")

	// CODEOWNERS and heuristics must succeed despite AI failure
	if s.Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical, got %q", s.Severity)
	}
	if s.Owner != "@checkout-team" {
		t.Errorf("expected owner @checkout-team, got %q", s.Owner)
	}
	if s.AISummary != nil {
		t.Errorf("expected nil AISummary on AI failure, got %+v", s.AISummary)
	}
}

func TestService_TimeoutResilience(t *testing.T) {
	fetcher := newMockFileFetcher()
	fetcher.setFile("org/app", ".github/CODEOWNERS", []byte("Features/Checkout/* @checkout-team\n"))
	codeownersProvider := NewCodeownersProvider(fetcher)

	// Mock AI hanging for 500ms
	ai := &mockSummarizer{
		delay: 500 * time.Millisecond,
		summary: &AISummary{
			Title:         "Too slow",
			ProbableCause: "Should not be returned",
		},
	}

	// Configure strict timeout of 50ms
	service := NewService(codeownersProvider, ai, WithTimeout(50*time.Millisecond))

	start := time.Now()
	s := service.TriageReport(context.Background(), sampleReport(), "org/app")
	elapsed := time.Since(start)

	if elapsed > 250*time.Millisecond {
		t.Errorf("TriageReport took too long: %v (expected ~50ms)", elapsed)
	}

	// Heuristics must be preserved even if enrichment timed out
	if s.Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical, got %q", s.Severity)
	}
	if s.AISummary != nil {
		t.Errorf("expected nil AISummary due to timeout, got %+v", s.AISummary)
	}
}

func TestService_NilOrUnconfiguredProviders(t *testing.T) {
	t.Run("nil service receiver", func(t *testing.T) {
		var s *Service
		got := s.TriageReport(context.Background(), sampleReport(), "org/app")
		if got.Severity != SeverityCritical {
			t.Errorf("expected SeverityCritical from nil service, got %q", got.Severity)
		}
	})

	t.Run("nil report input", func(t *testing.T) {
		s := NewService(nil, nil)
		got := s.TriageReport(context.Background(), nil, "org/app")
		if got.Severity != "" || len(got.Labels) != 0 {
			t.Errorf("expected empty Suggestion for nil report, got %+v", got)
		}
	})

	t.Run("empty repository string", func(t *testing.T) {
		fetcher := newMockFileFetcher()
		codeownersProvider := NewCodeownersProvider(fetcher)
		s := NewService(codeownersProvider, nil)

		got := s.TriageReport(context.Background(), sampleReport(), "")
		if got.Owner != "" {
			t.Errorf("expected empty owner when repository is empty, got %q", got.Owner)
		}
		if fetcher.totalCalls() != 0 {
			t.Errorf("expected 0 fetch calls for empty repository, got %d", fetcher.totalCalls())
		}
	})

	t.Run("unconfigured providers", func(t *testing.T) {
		s := NewService(nil, nil)
		got := s.TriageReport(context.Background(), sampleReport(), "org/app")
		if got.Severity != SeverityCritical {
			t.Errorf("expected SeverityCritical, got %q", got.Severity)
		}
		if got.Owner != "" || got.AISummary != nil {
			t.Errorf("expected unconfigured providers to leave Owner/AISummary empty, got %+v", got)
		}
	})
}

func TestService_ConcurrentRaceSafety(t *testing.T) {
	fetcher := newMockFileFetcher()
	fetcher.setFile("org/app", ".github/CODEOWNERS", []byte("Features/Checkout/* @checkout-team\n"))
	codeownersProvider := NewCodeownersProvider(fetcher)

	ai := &mockSummarizer{
		summary: &AISummary{
			Title:         "Title",
			ProbableCause: "Cause",
		},
	}

	service := NewService(codeownersProvider, ai)

	const concurrency = 25
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := service.TriageReport(context.Background(), sampleReport(), "org/app")
			if s.Severity != SeverityCritical {
				t.Errorf("expected SeverityCritical, got %q", s.Severity)
			}
		}()
	}
	wg.Wait()
}
