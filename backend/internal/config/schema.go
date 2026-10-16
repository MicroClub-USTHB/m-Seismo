package config

type ServerConfig struct {
	Host string
	Port int
}

type DatabaseConfig struct {
	URL      string
	MaxConns int32
}

type Config struct {
	Env      string
	LogLevel string
	Server   ServerConfig
	Database DatabaseConfig
}
