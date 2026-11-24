package observability

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuilder_GrafanaLink(t *testing.T) {
	anchor := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Window: 10 * time.Minute,
		Grafana: &GrafanaConfig{
			BaseURL:       "https://grafana.internal.net",
			DatasourceUID: "loki-prod",
			QueryTemplate: `{service="checkout"} |= "{session_id}"`,
		},
	}

	builder := NewBuilder(cfg)
	link, ok := builder.GrafanaLink("sess-123", anchor.Add(-10*time.Minute), anchor.Add(10*time.Minute))
	if !ok {
		t.Fatal("expected Grafana link to be generated")
	}

	if link.Provider != ProviderGrafana || link.Name != "Grafana" {
		t.Errorf("unexpected provider/name: %s / %s", link.Provider, link.Name)
	}

	parsed, err := url.Parse(link.URL)
	if err != nil {
		t.Fatalf("invalid URL generated: %v", err)
	}

	if parsed.Scheme != "https" || parsed.Host != "grafana.internal.net" || parsed.Path != "/explore" {
		t.Errorf("unexpected URL parts: %s", link.URL)
	}

	left := parsed.Query().Get("left")
	if left == "" {
		t.Fatal("missing 'left' parameter in query")
	}

	if !strings.Contains(left, "loki-prod") {
		t.Errorf("expected datasource loki-prod in left param: %s", left)
	}
	if !strings.Contains(left, `sess-123`) {
		t.Errorf("expected session_id to be substituted: %s", left)
	}
}

func TestBuilder_LokiLink(t *testing.T) {
	anchor := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Loki: &LokiConfig{
			BaseURL:       "https://loki.internal.net",
			QueryTemplate: `{container="api"} |= "$SESSION_ID"`,
		},
	}

	builder := NewBuilder(cfg)
	links := builder.BuildLinks("sess-abc-456", anchor)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}

	lokiLink := links[0]
	if lokiLink.Provider != ProviderLoki {
		t.Errorf("expected provider Loki, got %s", lokiLink.Provider)
	}
	if !strings.Contains(lokiLink.URL, "sess-abc-456") {
		t.Errorf("expected URL to contain session ID: %s", lokiLink.URL)
	}
}

func TestBuilder_CloudWatchLink(t *testing.T) {
	anchor := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Window: 15 * time.Minute,
		CloudWatch: &CloudWatchConfig{
			Region:   "eu-west-1",
			LogGroup: "/aws/ecs/mobile-api",
		},
	}

	builder := NewBuilder(cfg)
	link, ok := builder.CloudWatchLink("sess-789", anchor.Add(-15*time.Minute), anchor.Add(15*time.Minute))
	if !ok {
		t.Fatal("expected CloudWatch link")
	}

	if link.Provider != ProviderCloudWatch {
		t.Errorf("expected CloudWatch, got %s", link.Provider)
	}

	if !strings.Contains(link.URL, "eu-west-1") {
		t.Errorf("expected region in URL: %s", link.URL)
	}
	if !strings.Contains(link.URL, "sess-789") {
		t.Errorf("expected session ID in URL: %s", link.URL)
	}
	if !strings.Contains(link.URL, "mobile-api") {
		t.Errorf("expected log group in URL: %s", link.URL)
	}
}

func TestBuilder_DatadogLink(t *testing.T) {
	anchor := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Datadog: &DatadogConfig{
			Site:  "datadoghq.eu",
			Query: "service:payments env:prod",
		},
	}

	builder := NewBuilder(cfg)
	links := builder.BuildLinks("sess-xyz", anchor)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}

	dd := links[0]
	if dd.Provider != ProviderDatadog {
		t.Errorf("expected ProviderDatadog, got %s", dd.Provider)
	}

	parsed, err := url.Parse(dd.URL)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}

	if parsed.Host != "app.datadoghq.eu" {
		t.Errorf("expected datadoghq.eu, got %s", parsed.Host)
	}

	q := parsed.Query()
	queryParam := q.Get("query")
	if !strings.Contains(queryParam, "service:payments") || !strings.Contains(queryParam, "sess-xyz") {
		t.Errorf("unexpected query param: %s", queryParam)
	}
	if q.Get("live") != "false" {
		t.Errorf("expected live=false, got %s", q.Get("live"))
	}
}

func TestBuilder_EdgeCases(t *testing.T) {
	cfg := Config{
		Grafana: &GrafanaConfig{BaseURL: "https://grafana.test"},
		Datadog: &DatadogConfig{Site: "datadoghq.com"},
	}
	b := NewBuilder(cfg)

	t.Run("empty session ID returns nil", func(t *testing.T) {
		links := b.BuildLinks("", time.Now())
		if len(links) != 0 {
			t.Errorf("expected 0 links for empty session ID, got %d", len(links))
		}
	})

	t.Run("zero time returns nil", func(t *testing.T) {
		links := b.BuildLinks("session-123", time.Time{})
		if len(links) != 0 {
			t.Errorf("expected 0 links for zero time, got %d", len(links))
		}
	})

	t.Run("nil builder returns nil", func(t *testing.T) {
		var nilBuilder *Builder
		links := nilBuilder.BuildLinks("sess", time.Now())
		if len(links) != 0 {
			t.Errorf("expected 0 links for nil builder, got %d", len(links))
		}
	})

	t.Run("special characters in session ID are encoded safely", func(t *testing.T) {
		weirdID := `sess/123 "danger" & <tag>`
		links := b.BuildLinks(weirdID, time.Now())
		if len(links) == 0 {
			t.Fatal("expected links to be generated")
		}
		for _, link := range links {
			_, err := url.Parse(link.URL)
			if err != nil {
				t.Errorf("URL parsing failed for special characters: %v", err)
			}
		}
	})
}
