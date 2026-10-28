package crashlytics

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
)

// These tests need a migrated database and a Redis server. They are skipped
// unless SEISMO_TEST_DATABASE_URL / SEISMO_TEST_REDIS_URL are set.

func TestPGStore(t *testing.T) {
	url := os.Getenv("SEISMO_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SEISMO_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := db.New(pool)

	key := make([]byte, secrets.KeySize)
	rand.Read(key)
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPGStore(q, box)

	suffix := time.Now().Format("150405.000000")
	o, err := q.CreateOwner(ctx, db.CreateOwnerParams{Email: "crashlytics-test-" + suffix + "@example.com", PasswordHash: "x", Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	// Deleting the owner deletes its projects and their connections.
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM owners WHERE id = $1", o.ID) })
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerID: o.ID, Name: "test", Slug: "crashlytics-test"})
	if err != nil {
		t.Fatal(err)
	}
	projectID := p.ID.String()

	if ok, _ := store.OwnsProject(ctx, o.ID, projectID); !ok {
		t.Error("OwnsProject = false for the owner's project")
	}
	if ok, _ := store.OwnsProject(ctx, uuid.New(), projectID); ok {
		t.Error("OwnsProject = true for another owner")
	}
	if ok, _ := store.OwnsProject(ctx, o.ID, "not-a-uuid"); ok {
		t.Error("OwnsProject = true for an invalid ID")
	}
	if _, err := store.Get(ctx, projectID); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get before Save: err = %v, want ErrNotConnected", err)
	}
	if err := store.SetApps(ctx, projectID, []string{app}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("SetApps before Save: err = %v, want ErrNotConnected", err)
	}

	if err := store.Save(ctx, projectID, "bot@example.com", "refresh-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetApps(ctx, projectID, []string{app}); err != nil {
		t.Fatal(err)
	}
	// Reconnecting replaces the account and token but keeps the apps.
	if err := store.Save(ctx, projectID, "other@example.com", "refresh-2"); err != nil {
		t.Fatal(err)
	}

	conn, err := store.Get(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Email != "other@example.com" || conn.RefreshToken != "refresh-2" || len(conn.Apps) != 1 || conn.Apps[0] != app {
		t.Errorf("Get = %+v", conn)
	}

	var stored []byte
	if err := pool.QueryRow(ctx, "SELECT refresh_token FROM crashlytics_connections WHERE project_id = $1", p.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("refresh-2")) {
		t.Error("refresh token stored in clear")
	}

	conns, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range conns {
		found = found || c.ProjectID == projectID
	}
	if !found {
		t.Error("List does not return the connection")
	}
}

func TestRedisStates(t *testing.T) {
	url := os.Getenv("SEISMO_TEST_REDIS_URL")
	if url == "" {
		t.Skip("SEISMO_TEST_REDIS_URL not set")
	}
	ctx := context.Background()

	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	states := NewRedisStates(rdb)

	if err := states.Put(ctx, "state-1", project, time.Minute); err != nil {
		t.Fatal(err)
	}
	id, ok, err := states.Take(ctx, "state-1")
	if err != nil || !ok || id != project {
		t.Fatalf("Take = %q, %v, %v", id, ok, err)
	}
	if _, ok, _ := states.Take(ctx, "state-1"); ok {
		t.Error("state can be taken twice")
	}
	if _, ok, _ := states.Take(ctx, "unknown"); ok {
		t.Error("unknown state accepted")
	}
}
