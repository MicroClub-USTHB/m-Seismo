package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
)

// NOTE: needs TEST_DATABASE_URL. Wipes the tables, so point it at a scratch db only.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err := s.db.Exec(ctx, `TRUNCATE reports, project_keys, projects CASCADE`); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProjectFor(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	project := uuid.Must(uuid.NewV7())
	live := sha256.Sum256([]byte("live"))
	revoked := sha256.Sum256([]byte("revoked"))

	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.db.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, 'demo')`, project))
	must(s.db.Exec(ctx, `INSERT INTO project_keys (key_hash, project_id) VALUES ($1, $2)`, live[:], project))
	must(s.db.Exec(ctx, `INSERT INTO project_keys (key_hash, project_id, revoked_at) VALUES ($1, $2, now())`, revoked[:], project))

	got, err := s.ProjectFor(ctx, live[:])
	if err != nil || got != project {
		t.Fatalf("live key: got %v, %v", got, err)
	}
	if _, err := s.ProjectFor(ctx, revoked[:]); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked key: want ErrUnauthorized, got %v", err)
	}
	unknown := sha256.Sum256([]byte("nope"))
	if _, err := s.ProjectFor(ctx, unknown[:]); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unknown key: want ErrUnauthorized, got %v", err)
	}
}

func TestSaveIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	project := uuid.Must(uuid.NewV7())
	if _, err := s.db.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, 'demo')`, project); err != nil {
		t.Fatal(err)
	}
	r := Report{
		ID:             uuid.Must(uuid.NewV7()),
		InstallationID: "inst-1",
		Mode:           "QA",
		Trigger:        "shake",
	}
	for range 2 {
		if err := s.Save(ctx, project, r); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM reports WHERE id = $1`, r.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 row after a repeat save, got %d", n)
	}
}
