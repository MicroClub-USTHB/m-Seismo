package observability

import (
	"time"
)

// Provider identifies an observability platform.
type Provider string

const (
	ProviderGrafana    Provider = "grafana"
	ProviderLoki       Provider = "loki"
	ProviderCloudWatch Provider = "cloudwatch"
	ProviderDatadog    Provider = "datadog"
)

// Link represents a deep-link to an external observability tool.
type Link struct {
	Provider Provider `json:"provider"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
}

// Config specifies configurations for supported observability platforms.
type Config struct {
	// Window determines the time delta around occurred_at (default: 15 minutes).
	// The query interval will be [occurred_at - Window, occurred_at + Window].
	Window time.Duration `json:"window"`

	Grafana    *GrafanaConfig    `json:"grafana,omitempty"`
	Loki       *LokiConfig       `json:"loki,omitempty"`
	CloudWatch *CloudWatchConfig `json:"cloudwatch,omitempty"`
	Datadog    *DatadogConfig    `json:"datadog,omitempty"`
}

// GrafanaConfig configures Grafana Explore links.
type GrafanaConfig struct {
	BaseURL       string `json:"base_url"`                 // e.g. "https://grafana.example.com"
	DatasourceUID string `json:"datasource_uid,omitempty"` // optional datasource UID
	QueryTemplate string `json:"query_template,omitempty"` // optional query template with {session_id} placeholder
}

// LokiConfig configures Loki links.
type LokiConfig struct {
	BaseURL       string `json:"base_url"`                 // e.g. "https://grafana.example.com" or Loki host
	DatasourceUID string `json:"datasource_uid,omitempty"` // optional Loki datasource UID
	QueryTemplate string `json:"query_template,omitempty"` // e.g. '{app="my-app"} |= "{session_id}"'
}

// CloudWatchConfig configures AWS CloudWatch Log Insights links.
type CloudWatchConfig struct {
	Region   string `json:"region"`             // e.g. "us-east-1"
	LogGroup string `json:"log_group"`          // e.g. "/aws/container/api"
	BaseURL  string `json:"base_url,omitempty"` // optional console URL override
}

// DatadogConfig configures Datadog Log Explorer links.
type DatadogConfig struct {
	Site    string `json:"site,omitempty"`     // e.g. "datadoghq.com" or "datadoghq.eu" (default: "datadoghq.com")
	BaseURL string `json:"base_url,omitempty"` // optional full URL override
	Query   string `json:"query,omitempty"`    // base query prefix (e.g. "service:mobile-backend")
}
