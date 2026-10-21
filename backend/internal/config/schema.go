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

// CrashConfig selects the crash reporter reports are linked to.
type CrashConfig struct {
	// Source is "none" or "crashlytics".
	Source string
	// Window is how far apart a report and a crash may be and still match.
	Window      time.Duration
	Crashlytics CrashlyticsConfig
}

// CrashlyticsConfig identifies m-Seismo to Google as an OAuth client. Each
// project's own access (refresh token, Firebase apps) is stored in the
// database by the OAuth flow, not here.
type CrashlyticsConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the public URL of /api/v1/oauth/crashlytics/callback.
	// Empty means server.public_url plus that path.
	RedirectURL  string
	QuotaProject string
}

type SecurityConfig struct {
	// SecretKey (base64, 32 bytes) encrypts secrets stored in the database.
	SecretKey string
}

type Config struct {
	Env      string
	LogLevel string
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
	Auth     AuthConfig
	GitHub   GitHubConfig
	Limits   LimitsConfig
	Ingest   IngestConfig
	Crash    CrashConfig
	Security SecurityConfig
}
