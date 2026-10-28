package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// This test needs a migrated database. It is skipped unless
// SEISMO_TEST_DATABASE_URL is set.
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

	owner, err := q.CreateOwner(ctx, db.CreateOwnerParams{
		Email: fmt.Sprintf("ingest-%d@example.com", time.Now().UnixNano()), PasswordHash: "x", Name: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM owners WHERE id = $1", owner.ID); err != nil {
			t.Error(err)
		}
	})
	app, err := q.CreateApp(ctx, db.CreateAppParams{OwnerID: owner.ID, Name: "app", Platform: db.AppPlatformIos, BundleID: "com.example"})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(specDir + "/examples/shake-report.json")
	r, err := ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	r.ID = uuid.NewString()
	payload, _ := json.Marshal(r)
	shot := []Attachment{{Name: "screenshot", ContentType: "image/png", Data: []byte("png")}}

	store := NewPGStore(pool)

	first, err := store.Save(ctx, app.ID, r, payload, shot)
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate || first.Queued {
		t.Fatalf("first save = %+v", first)
	}

	// The same report again: same row, nothing new written.
	again, err := store.Save(ctx, app.ID, r, payload, shot)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Duplicate || again.ID != first.ID || again.Queued {
		t.Fatalf("second save = %+v, want a duplicate of %s not yet queued", again, first.ID)
	}

	if err := store.MarkQueued(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	third, err := store.Save(ctx, app.ID, r, payload, shot)
	if err != nil {
		t.Fatal(err)
	}
	if !third.Duplicate || !third.Queued {
		t.Fatalf("third save = %+v, want a queued duplicate", third)
	}

	var reports, files int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM reports WHERE app_id = $1", app.ID).Scan(&reports); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM report_attachments WHERE report_id = $1", first.ID).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || files != 1 {
		t.Errorf("stored %d reports and %d attachments, want 1 and 1", reports, files)
	}

	var stored map[string]any
	if err := pool.QueryRow(ctx, "SELECT payload FROM reports WHERE id = $1", first.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored["installation_id"] != r.InstallationID {
		t.Errorf("payload not stored as sent: %v", stored["installation_id"])
	}
}
