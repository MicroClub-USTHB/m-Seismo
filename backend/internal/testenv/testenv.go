// Package testenv gives integration tests their Postgres, Redis and NATS.
//
// Each helper returns the URL in SEISMO_TEST_DATABASE_URL, SEISMO_TEST_REDIS_URL
// or SEISMO_TEST_NATS_URL when it is set, so tests can still run against
// services you started yourself. Otherwise it starts a throwaway container
// with testcontainers, once per test binary, and skips the test when Docker
// is not available. Containers are removed when the test binary exits.
package testenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const startTimeout = 2 * time.Minute

// service is a container started at most once per test binary.
type service struct {
	once sync.Once
	url  string
	err  error
}

var postgres, redis, nats service

// DatabaseURL is a Postgres with every migration applied.
func DatabaseURL(t testing.TB) string {
	t.Helper()
	return get(t, "SEISMO_TEST_DATABASE_URL", &postgres, startPostgres)
}

// RedisURL is a Redis server.
func RedisURL(t testing.TB) string {
	t.Helper()
	return get(t, "SEISMO_TEST_REDIS_URL", &redis, startRedis)
}

// NATSURL is a NATS server with JetStream.
func NATSURL(t testing.TB) string {
	t.Helper()
	return get(t, "SEISMO_TEST_NATS_URL", &nats, startNATS)
}

func get(t testing.TB, env string, s *service, start func(context.Context) (string, error)) string {
	t.Helper()
	if url := os.Getenv(env); url != "" {
		return url
	}
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		s.url, s.err = start(ctx)
	})
	if s.err != nil {
		t.Skipf("%s is not set and a container could not be started: %v", env, s.err)
	}
	return s.url
}

func startPostgres(ctx context.Context) (string, error) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "seismo", "POSTGRES_PASSWORD": "seismo", "POSTGRES_DB": "seismo"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(startTimeout),
		},
		Started: true,
	})
	if err != nil {
		return "", err
	}
	url, err := endpoint(ctx, c, "5432/tcp", "postgres://seismo:seismo@%s/seismo?sslmode=disable")
	if err != nil {
		return "", err
	}
	if err := migrate(ctx, url); err != nil {
		return "", err
	}
	return url, nil
}

func startRedis(ctx context.Context) (string, error) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(startTimeout),
		},
		Started: true,
	})
	if err != nil {
		return "", err
	}
	return endpoint(ctx, c, "6379/tcp", "redis://%s/0")
}

func startNATS(ctx context.Context) (string, error) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nats:2-alpine",
			Cmd:          []string{"-js"},
			ExposedPorts: []string{"4222/tcp"},
			WaitingFor:   wait.ForLog("Server is ready").WithStartupTimeout(startTimeout),
		},
		Started: true,
	})
	if err != nil {
		return "", err
	}
	return endpoint(ctx, c, "4222/tcp", "nats://%s")
}

// endpoint formats the container's host:port into format.
func endpoint(ctx context.Context, c testcontainers.Container, port, format string) (string, error) {
	hostPort, err := c.PortEndpoint(ctx, port, "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(format, hostPort), nil
}

// migrate applies the "migrate:up" part of every file in db/migrations, in
// file name order, as dbmate does.
func migrate(ctx context.Context, url string) error {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "db", "migrations")
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // migration files of this repository
		if err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, upSection(string(raw))); err != nil {
			return fmt.Errorf("apply %s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

func upSection(s string) string {
	s = s[strings.Index(s, "-- migrate:up")+len("-- migrate:up"):]
	if i := strings.Index(s, "-- migrate:down"); i >= 0 {
		s = s[:i]
	}
	return s
}
