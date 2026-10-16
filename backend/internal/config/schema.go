package config

type ServerConfig struct {
	Host string
	Port int
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

type Config struct {
	Env      string
	LogLevel string
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
}
