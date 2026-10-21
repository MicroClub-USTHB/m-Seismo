package config

import "time"

type ServerConfig struct {
	Host string
	Port int
	// PublicURL is the address the outside world reaches this server on, used
	// to build callback URLs (e.g. https://seismo.example.com).
	PublicURL string
	// FrontendURL is where the browser is sent back to after a provider
	// connect flow.
	FrontendURL string
}

type DatabaseConfig struct {
	URL      string
	MaxConns int32
}

type RedisConfig struct {
	URL string
}

type NATSConfig struct {
	URL string
}

type AuthConfig struct {
	JWTSecret string
	JWTTTL    time.Duration
}

// GitHubConfig holds the GitHub App credentials m-Seismo uses to reach
// owners' repositories.
type GitHubConfig struct {
	AppID        int64
	AppSlug      string
	ClientID     string
	ClientSecret string
	// PrivateKey is the App's PEM private key. PrivateKeyPath is read when
	// PrivateKey is empty.
	PrivateKey     string
	PrivateKeyPath string
	// APIURL is the REST API base URL; override it for GitHub Enterprise or
	// tests.
	APIURL string
	// WebURL is the github.com web base URL used for install and OAuth links.
	WebURL string
	// MinWriteInterval spaces write requests (opening issues, comments) per
	// installation, as GitHub asks to avoid secondary rate limits.
	MinWriteInterval time.Duration
	// MaxRateLimitWait is the longest one call waits out a GitHub rate limit
	// before failing with a retryable rate-limit error.
	MaxRateLimitWait time.Duration
}

// LimitsConfig holds m-Seismo's own request limits.
type LimitsConfig struct {
	// TestIssuesPerMinute caps POST /apps/{id}/test-issue per app, so the
	// action cannot be used to burn the tracker's quota.
	TestIssuesPerMinute int
}

// IngestConfig holds the rate limits on reports sent by the SDKs.
type IngestConfig struct {
	// AppPerMinute and AppBurst limit all reports of one app.
	AppPerMinute float64
	AppBurst     int
	// InstallationPerMinute and InstallationBurst limit the reports of one
	// installation of an app.
	InstallationPerMinute float64
	InstallationBurst     int
}

// WorkerConfig controls the pipeline worker that turns accepted reports
// into issues.
type WorkerConfig struct {
	// Enabled runs the worker in this process. Turn it off to run the API
	// alone and the workers elsewhere.
	Enabled bool
	// Concurrency is how many reports one process works on at a time.
	Concurrency int
	// MaxAttempts is how many times a report is tried before the worker
	// gives up on it. Waiting out a tracker's rate limit counts as an
	// attempt.
	MaxAttempts int
}

// CoordinatorConfig controls the gRPC server the log coordinator attaches
// to, and the server logs the worker asks it for.
type CoordinatorConfig struct {
	// Enabled serves Coordinator.Attach in this process.
	Enabled bool
	// Addr is where the gRPC server listens, e.g. ":9443".
	Addr string
	// TLSCertFile and TLSKeyFile hold the server certificate. Without them
	// the server speaks plain gRPC, which is only allowed in development
	// or when Plaintext says TLS ends in front of the backend.
	TLSCertFile string
	TLSKeyFile  string
	Plaintext   bool
	// Window is how far before and after a report the worker asks for
	// server logs.
	Window time.Duration
	// Timeout is how long the worker waits for the coordinator's answer.
	Timeout time.Duration
	// MaxBytes is the log budget of one query, for the whole aggregate.
	MaxBytes int
	// MaxLinesPerWorkload caps the lines of each service.
	MaxLinesPerWorkload int
}

type Config struct {
	Env         string
	LogLevel    string
	Server      ServerConfig
	Database    DatabaseConfig
	Redis       RedisConfig
	NATS        NATSConfig
	Auth        AuthConfig
	GitHub      GitHubConfig
	Limits      LimitsConfig
	Ingest      IngestConfig
	Worker      WorkerConfig
	Coordinator CoordinatorConfig
}
