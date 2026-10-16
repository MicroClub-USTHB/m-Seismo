// Package testutil gives integration tests a freshly migrated database.
//
// Set TEST_DATABASE_URL to use an existing server (the role needs CREATEDB);
// otherwise a PostgreSQL container is started once per test binary. Tests
// are skipped when neither is available.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

var (
	serverOnce sync.Once
	serverURL  string
	serverErr  error
)

func adminURL() (string, error) {
	serverOnce.Do(func() {
		if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
			serverURL = u
			return
		}
		serverURL, serverErr = startContainer()
	})
	return serverURL, serverErr
}

func startContainer() (u string, err error) {
	defer func() {
		// testcontainers panics when no Docker daemon is reachable.
		if r := recover(); r != nil {
			err = fmt.Errorf("start postgres container: %v", r)
		}
	}()
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		return "", fmt.Errorf("start postgres container: %w", err)
	}
	// The container is reaped by testcontainers' Ryuk when the process exits.
	return c.ConnectionString(ctx, "sslmode=disable")
}

// NewDB creates an empty database, migrates it and drops it when the test
// ends.
func NewDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	base, err := adminURL()
	if err != nil {
		t.Skipf("no test database: %v (set TEST_DATABASE_URL or start Docker)", err)
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Skipf("no test database: %v", err)
	}
	defer admin.Close(ctx)

	name := "mseismo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	dbURL, err := withDatabase(base, name)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		conn, err := pgx.Connect(context.Background(), base)
		if err != nil {
			t.Logf("drop test database: %v", err)
			return
		}
		defer conn.Close(context.Background())
		if _, err := conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database: %v", err)
		}
	})
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func withDatabase(raw, name string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	u.Path = "/" + name
	return u.String(), nil
}
