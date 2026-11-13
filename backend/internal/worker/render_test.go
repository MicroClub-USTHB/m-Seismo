package worker

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/triage"
)

func TestTitle(t *testing.T) {
	tests := []struct {
		name string
		r    ingest.Report
		want string
	}{
		{"reporter title", ingest.Report{UserInput: &ingest.UserInput{Title: "  Pay\nbutton  "}}, "Pay button"},
		{"crash", ingest.Report{Crash: &ingest.Crash{Type: "NSInvalidArgumentException", Reason: "nil key"}}, "Crash: NSInvalidArgumentException: nil key"},
		{"generated", ingest.Report{Platform: "android", Trigger: "shake", Release: &ingest.Release{Version: "2.3.1"}}, "Report from android 2.3.1 (shake)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := render(&tt.r, reportInfo{}).Title; got != tt.want {
				t.Errorf("title = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderBody(t *testing.T) {
	r := ingest.Report{
		Platform:  "ios",
		Trigger:   "shake",
		UserInput: &ingest.UserInput{Description: "ping @octocat", Category: "bug", Severity: "high"},
		Device:    &ingest.Device{Model: "iPhone15,2", OSName: "iOS", OSVersion: "18.1"},
		Release:   &ingest.Release{Version: "2.3.1", Build: "87"},
		Breadcrumbs: []ingest.Breadcrumb{
			{Kind: "navigation", Message: "HomeScreen"},
			{Kind: "tap", Message: "Pay"},
		},
		Logs: []ingest.LogLine{{Timestamp: "2026-10-21T10:00:00.000Z", Level: "error", Message: "payment failed ``` here"}},
		NetworkRequests: []ingest.NetworkRequest{
			{Method: "POST", URL: "https://api.example.com/pay", ResponseBody: "secret-token"},
		},
	}
	id, inst := uuid.New(), uuid.New()
	body := render(&r, reportInfo{ID: id, InstallationID: inst, ScreenshotURL: "https://seismo.example.com/api/v1/attachments/1?expires=1&sig=abc"}).Body

	for _, want := range []string{
		"ping @\u200boctocat",
		"**Category:** bug · **Severity:** high",
		"1. HomeScreen _(navigation)_\n2. Pay _(tap)_",
		"| Device | iPhone15,2 |",
		"| Release | 2.3.1 (87) |",
		"````\n2026-10-21T10:00:00.000Z ERROR payment failed ``` here\n````",
		"POST https://api.example.com/pay → no response",
		"[Screenshot](https://seismo.example.com/api/v1/attachments/1?expires=1&sig=abc)",
		id.String(),
		inst.String(),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "secret-token") {
		t.Error("body contains a network request body")
	}
}

func TestTruncate(t *testing.T) {
	s := strings.Repeat("é", 100) // 2 bytes each
	got := truncate(s, 51, "…")
	if len(got) > 51 || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Errorf("truncate = %q (%d bytes)", got, len(got))
	}
	if truncate("short", 10, "…") != "short" {
		t.Error("short string changed")
	}
}

func TestRender_AutoTriageSection(t *testing.T) {
	r := &ingest.Report{
		Platform: "ios",
		Trigger:  "crash",
	}
	info := reportInfo{
		ID:             uuid.New(),
		InstallationID: uuid.New(),
		Triage: triage.Suggestion{
			Severity:       "critical",
			SeverityReason: "fatal crash",
			Labels:         []string{"ios", "crash"},
			Owner:          "@checkout-team",
			OwnerReason:    "matched CODEOWNERS rule 'Features/Checkout/*'",
			Screen:         "CheckoutScreen",
		},
	}

	draft := render(r, info)
	if !strings.Contains(draft.Body, "Auto-Triage Suggestions") {
		t.Errorf("expected Body to contain 'Auto-Triage Suggestions', got:\n%s", draft.Body)
	}
	if !strings.Contains(draft.Body, "Suggested Severity: critical") {
		t.Errorf("expected Body to contain 'Suggested Severity: critical', got:\n%s", draft.Body)
	}
	if !strings.Contains(draft.Body, "Suggested Owner: @checkout-team") {
		t.Errorf("expected Body to contain 'Suggested Owner: @checkout-team', got:\n%s", draft.Body)
	}
	if !strings.Contains(draft.Body, "`ios`, `crash`") {
		t.Errorf("expected Body to contain formatted labels, got:\n%s", draft.Body)
	}
	if !strings.Contains(draft.Body, "Active Screen: CheckoutScreen") {
		t.Errorf("expected Body to contain active screen, got:\n%s", draft.Body)
	}
}
