package observability

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DefaultWindow is the default time margin applied around occurred_at.
const DefaultWindow = 15 * time.Minute

// Builder constructs deep-links to external observability tools.
type Builder struct {
	cfg Config
}

// NewBuilder creates a new Builder with the given configuration.
func NewBuilder(cfg Config) *Builder {
	if cfg.Window <= 0 {
		cfg.Window = DefaultWindow
	}
	return &Builder{cfg: cfg}
}

// BuildLinks produces all configured observability links for a session and timestamp.
// If sessionID is empty or occurredAt is zero, it returns nil.
func (b *Builder) BuildLinks(sessionID string, occurredAt time.Time) []Link {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || occurredAt.IsZero() || b == nil {
		return nil
	}

	from := occurredAt.Add(-b.cfg.Window)
	to := occurredAt.Add(b.cfg.Window)

	var links []Link

	if link, ok := b.GrafanaLink(sessionID, from, to); ok {
		links = append(links, link)
	}
	if link, ok := b.LokiLink(sessionID, from, to); ok {
		links = append(links, link)
	}
	if link, ok := b.CloudWatchLink(sessionID, from, to); ok {
		links = append(links, link)
	}
	if link, ok := b.DatadogLink(sessionID, from, to); ok {
		links = append(links, link)
	}

	return links
}

// GrafanaLink builds an Explore deep-link for Grafana.
func (b *Builder) GrafanaLink(sessionID string, from, to time.Time) (Link, bool) {
	if b == nil || b.cfg.Grafana == nil {
		return Link{}, false
	}
	baseURL := strings.TrimRight(strings.TrimSpace(b.cfg.Grafana.BaseURL), "/")
	if baseURL == "" {
		return Link{}, false
	}

	query := fmt.Sprintf("{session_id=%q}", sessionID)
	if b.cfg.Grafana.QueryTemplate != "" {
		query = applyTemplate(b.cfg.Grafana.QueryTemplate, sessionID)
	}

	left := map[string]any{
		"range": map[string]string{
			"from": fmt.Sprintf("%d", from.UnixMilli()),
			"to":   fmt.Sprintf("%d", to.UnixMilli()),
		},
		"queries": []map[string]any{
			{
				"refId": "A",
				"expr":  query,
			},
		},
	}
	if b.cfg.Grafana.DatasourceUID != "" {
		left["datasource"] = b.cfg.Grafana.DatasourceUID
	}

	leftBytes, err := json.Marshal(left)
	if err != nil {
		return Link{}, false
	}

	linkURL := fmt.Sprintf("%s/explore?left=%s", baseURL, url.QueryEscape(string(leftBytes)))
	return Link{
		Provider: ProviderGrafana,
		Name:     "Grafana",
		URL:      linkURL,
	}, true
}

// LokiLink builds a deep-link for Loki logs.
func (b *Builder) LokiLink(sessionID string, from, to time.Time) (Link, bool) {
	if b == nil || b.cfg.Loki == nil {
		return Link{}, false
	}
	baseURL := strings.TrimRight(strings.TrimSpace(b.cfg.Loki.BaseURL), "/")
	if baseURL == "" {
		return Link{}, false
	}

	query := fmt.Sprintf(`{app=~".+"} |= %q`, sessionID)
	if b.cfg.Loki.QueryTemplate != "" {
		query = applyTemplate(b.cfg.Loki.QueryTemplate, sessionID)
	}

	left := map[string]any{
		"range": map[string]string{
			"from": fmt.Sprintf("%d", from.UnixMilli()),
			"to":   fmt.Sprintf("%d", to.UnixMilli()),
		},
		"queries": []map[string]any{
			{
				"refId": "A",
				"expr":  query,
			},
		},
	}
	if b.cfg.Loki.DatasourceUID != "" {
		left["datasource"] = b.cfg.Loki.DatasourceUID
	}

	leftBytes, err := json.Marshal(left)
	if err != nil {
		return Link{}, false
	}

	linkURL := fmt.Sprintf("%s/explore?left=%s", baseURL, url.QueryEscape(string(leftBytes)))
	return Link{
		Provider: ProviderLoki,
		Name:     "Loki",
		URL:      linkURL,
	}, true
}

// CloudWatchLink builds an AWS CloudWatch Log Insights / Log Events deep-link.
func (b *Builder) CloudWatchLink(sessionID string, from, to time.Time) (Link, bool) {
	if b == nil || b.cfg.CloudWatch == nil {
		return Link{}, false
	}
	logGroup := strings.TrimSpace(b.cfg.CloudWatch.LogGroup)
	if logGroup == "" {
		return Link{}, false
	}

	region := strings.TrimSpace(b.cfg.CloudWatch.Region)
	if region == "" {
		region = "us-east-1"
	}

	baseURL := strings.TrimRight(strings.TrimSpace(b.cfg.CloudWatch.BaseURL), "/")
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.console.aws.amazon.com/cloudwatch/home", region)
	}

	// AWS CloudWatch console URL fragment format for log group event search:
	// #logsV2:log-groups/log-group/<escapedGroup>?start=<from_ms>&end=<to_ms>&filter=<escapedSessionID>
	// In CloudWatch console URLs, hash fragments encode '?' as '$3F', '&' as '$26', and '=' as '$3D'.
	escapedGroup := strings.ReplaceAll(url.PathEscape(logGroup), "%2F", "*2f")
	filterParam := url.QueryEscape(sessionID)

	fragment := fmt.Sprintf("logsV2:log-groups/log-group/%s$3Fstart$3D%d$26end$3D%d$26filter$3D%s",
		escapedGroup,
		from.UnixMilli(),
		to.UnixMilli(),
		filterParam,
	)

	linkURL := fmt.Sprintf("%s?region=%s#%s", baseURL, region, fragment)
	return Link{
		Provider: ProviderCloudWatch,
		Name:     "CloudWatch",
		URL:      linkURL,
	}, true
}

// DatadogLink builds a Datadog Logs Explorer deep-link.
func (b *Builder) DatadogLink(sessionID string, from, to time.Time) (Link, bool) {
	if b == nil || b.cfg.Datadog == nil {
		return Link{}, false
	}

	site := strings.TrimSpace(b.cfg.Datadog.Site)
	if site == "" {
		site = "datadoghq.com"
	}

	baseURL := strings.TrimRight(strings.TrimSpace(b.cfg.Datadog.BaseURL), "/")
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://app.%s/logs", site)
	}

	query := sessionID
	if strings.TrimSpace(b.cfg.Datadog.Query) != "" {
		query = fmt.Sprintf("%s %s", strings.TrimSpace(b.cfg.Datadog.Query), sessionID)
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("from_ts", fmt.Sprintf("%d", from.UnixMilli()))
	params.Set("to_ts", fmt.Sprintf("%d", to.UnixMilli()))
	params.Set("live", "false")

	linkURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())
	return Link{
		Provider: ProviderDatadog,
		Name:     "Datadog",
		URL:      linkURL,
	}, true
}

func applyTemplate(tpl, sessionID string) string {
	res := strings.ReplaceAll(tpl, "{session_id}", sessionID)
	res = strings.ReplaceAll(res, "$SESSION_ID", sessionID)
	return res
}
