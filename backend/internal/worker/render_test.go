package worker

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/observability"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/release"
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

func TestRender_ObservabilityLinks(t *testing.T) {
	r := ingest.Report{
		Platform: "android",
		Trigger:  "shake",
	}

	links := []observability.Link{
		{Provider: observability.ProviderGrafana, Name: "Grafana", URL: "https://grafana.example.com/explore?left=1"},
		{Provider: observability.ProviderLoki, Name: "Loki", URL: "https://grafana.example.com/explore?left=2"},
		{Provider: observability.ProviderCloudWatch, Name: "CloudWatch", URL: "https://console.aws.amazon.com/cloudwatch"},
		{Provider: observability.ProviderDatadog, Name: "Datadog", URL: "https://app.datadoghq.com/logs"},
	}

	body := render(&r, reportInfo{ObservabilityLinks: links}).Body

	wantSection := "### Observability\n\n" +
		"* [Grafana](https://grafana.example.com/explore?left=1)\n" +
		"* [Loki](https://grafana.example.com/explore?left=2)\n" +
		"* [CloudWatch](https://console.aws.amazon.com/cloudwatch)\n" +
		"* [Datadog](https://app.datadoghq.com/logs)\n"

	if !strings.Contains(body, wantSection) {
		t.Errorf("body missing observability section:\n%s", body)
	}

	// Empty links produces no section
	bodyNoLinks := render(&r, reportInfo{}).Body
	if strings.Contains(bodyNoLinks, "### Observability") {
		t.Errorf("body unexpectedly contains observability section when links is empty:\n%s", bodyNoLinks)
	}
}

func TestRender_ReleaseCorrelation(t *testing.T) {
	r := ingest.Report{
		Platform: "ios",
		Trigger:  "shake",
		Release:  &ingest.Release{Version: "2.3.1", Build: "87"},
	}

	rc := &release.CorrelatedRelease{
		Version:    "2.3.1",
		Build:      "87",
		TagName:    "v2.3.1",
		ReleaseURL: "https://github.com/org/repo/releases/tag/v2.3.1",
		CompareURL: "https://github.com/org/repo/compare/v2.3.0...v2.3.1",
		CommitURL:  "https://github.com/org/repo/commit/87abcde",
	}

	body := render(&r, reportInfo{ReleaseCorrelation: rc}).Body

	wantRow := "| Release | 2.3.1 (87) · [Release Notes](https://github.com/org/repo/releases/tag/v2.3.1) · [Compare Changes](https://github.com/org/repo/compare/v2.3.0...v2.3.1) · [Commit](https://github.com/org/repo/commit/87abcde) |"
	if !strings.Contains(body, wantRow) {
		t.Errorf("body missing release correlation row:\nwant: %s\ngot:\n%s", wantRow, body)
	}

	// Without correlation, retains standard format
	bodyNoCorr := render(&r, reportInfo{}).Body
	wantStandard := "| Release | 2.3.1 (87) |"
	if !strings.Contains(bodyNoCorr, wantStandard) {
		t.Errorf("body missing standard release row:\n%s", bodyNoCorr)
	}
}

func TestRender_TemplateObservability(t *testing.T) {
	r := ingest.Report{
		Platform:  "ios",
		Trigger:   "shake",
		UserInput: &ingest.UserInput{Title: "Crash on pay"},
	}

	links := []observability.Link{
		{Provider: observability.ProviderGrafana, Name: "Grafana", URL: "https://grafana.example.com/explore"},
	}

	titleTpl := "Issue: {{.Title}}"
	bodyTpl := "{{.Observability}}"

	draft, err := renderWithTemplate(titleTpl, bodyTpl, &r, reportInfo{ObservabilityLinks: links})
	if err != nil {
		t.Fatalf("renderWithTemplate failed: %v", err)
	}

	if draft.Title != "Issue: Crash on pay" {
		t.Errorf("title = %q, want 'Issue: Crash on pay'", draft.Title)
	}
	if !strings.Contains(draft.Body, "### Observability") || !strings.Contains(draft.Body, "* [Grafana](https://grafana.example.com/explore)") {
		t.Errorf("template body missing observability section:\n%s", draft.Body)
	}
}
