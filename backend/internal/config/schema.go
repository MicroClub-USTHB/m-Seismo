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
}
