package triage

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// DefaultTimeout is the default bounded duration for triage enrichment.
const DefaultTimeout = 3 * time.Second

// SummaryInput contains the extracted report facts passed to an AI Summarizer.
type SummaryInput struct {
	Platform    string   `json:"platform"`
	Trigger     string   `json:"trigger"`
	CrashType   string   `json:"crash_type,omitempty"`
	CrashReason string   `json:"crash_reason,omitempty"`
	StackFrames []string `json:"stack_frames,omitempty"`
	Breadcrumbs []string `json:"breadcrumbs,omitempty"`
	UserTitle   string   `json:"user_title,omitempty"`
	UserDesc    string   `json:"user_description,omitempty"`
}

// Summarizer generates a suggested title and probable root cause analysis.
type Summarizer interface {
	Summarize(ctx context.Context, input SummaryInput) (*AISummary, error)
}

// Service orchestrates triage suggestions across heuristics, CODEOWNERS,
// and optional AI summarization with bounded latency and graceful degradation.
type Service struct {
	codeowners *CodeownersProvider
	summarizer Summarizer
	timeout    time.Duration
}

// ServiceOption configures a Service.
type ServiceOption func(*Service)

// WithTimeout sets a custom maximum duration for triage enrichment.
func WithTimeout(d time.Duration) ServiceOption {
	return func(s *Service) {
		if d > 0 {
			s.timeout = d
		}
	}
}

// NewService creates a new concurrent triage service.
func NewService(codeowners *CodeownersProvider, summarizer Summarizer, opts ...ServiceOption) *Service {
	s := &Service{
		codeowners: codeowners,
		summarizer: summarizer,
		timeout:    DefaultTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// TriageReport enriches a report with heuristics, CODEOWNERS matching,
// and optional AI summary, bound by a strict context timeout.
func (s *Service) TriageReport(ctx context.Context, r *ingest.Report, repository string) Suggestion {
	if r == nil {
		return Suggestion{}
	}

	// Step 1: Instant in-memory heuristics (CPU only, cannot block)
	suggestion := Analyze(r)

	if s == nil {
		return suggestion
	}

	// Step 2: Bounded context timeout
	timeout := s.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	screen := suggestion.Screen

	// Step 3: Fan-Out with sync.WaitGroup
	// Goroutine A: CODEOWNERS matching
	if s.codeowners != nil && repository != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			co, err := s.codeowners.GetCodeowners(tCtx, repository)
			if err == nil && co != nil {
				owner, reason := MatchOwner(co, r, screen)
				if owner != "" {
					mu.Lock()
					suggestion.Owner = owner
					suggestion.OwnerReason = reason
					mu.Unlock()
				}
			}
		}()
	}

	// Goroutine B: AI Summarizer
	if s.summarizer != nil {
		input := buildSummaryInput(r)
		wg.Add(1)
		go func() {
			defer wg.Done()
			summary, err := s.summarizer.Summarize(tCtx, input)
			if err == nil && summary != nil {
				mu.Lock()
				suggestion.AISummary = summary
				mu.Unlock()
			}
		}()
	}

	// Step 4: Fan-In with Non-Blocking Timeout Exit
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All background tasks finished within timeout
	case <-tCtx.Done():
		// Timed out: return whatever has been completed so far
	}

	mu.Lock()
	out := suggestion
	mu.Unlock()
	return out
}

func buildSummaryInput(r *ingest.Report) SummaryInput {
	if r == nil {
		return SummaryInput{}
	}

	input := SummaryInput{
		Platform: r.Platform,
		Trigger:  r.Trigger,
	}

	if r.UserInput != nil {
		input.UserTitle = r.UserInput.Title
		input.UserDesc = r.UserInput.Description
	}

	if r.Crash != nil {
		input.CrashType = r.Crash.Type
		input.CrashReason = r.Crash.Reason
		if len(r.Crash.StackTrace) > 0 {
			limit := min(len(r.Crash.StackTrace), 3)
			input.StackFrames = slices.Clone(r.Crash.StackTrace[:limit])
		}
	}

	if len(r.Breadcrumbs) > 0 {
		crumbs := r.Breadcrumbs
		if len(crumbs) > 5 {
			crumbs = crumbs[len(crumbs)-5:]
		}
		msgs := make([]string, len(crumbs))
		for i, b := range crumbs {
			if b.Kind != "" {
				msgs[i] = fmt.Sprintf("[%s] %s", b.Kind, b.Message)
			} else {
				msgs[i] = b.Message
			}
		}
		input.Breadcrumbs = msgs
	}

	return input
}
