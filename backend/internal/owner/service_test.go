package owner

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/auth"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// fakeQuerier is an in-memory db.Querier covering only what owner.Service
// calls; everything else panics through the embedded nil interface.
type fakeQuerier struct {
	db.Querier
	byEmail map[string]db.Owner
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{byEmail: map[string]db.Owner{}}
}

func (f *fakeQuerier) CreateOwner(_ context.Context, arg db.CreateOwnerParams) (db.Owner, error) {
	if _, exists := f.byEmail[arg.Email]; exists {
		return db.Owner{}, &pgconn.PgError{Code: "23505"}
	}
	o := db.Owner{ID: uuid.New(), Email: arg.Email, PasswordHash: arg.PasswordHash, Name: arg.Name, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.byEmail[arg.Email] = o
	return o, nil
}

func (f *fakeQuerier) GetOwnerByEmail(_ context.Context, email string) (db.Owner, error) {
	if o, ok := f.byEmail[email]; ok {
		return o, nil
	}
	return db.Owner{}, pgx.ErrNoRows
}

func newTestService(t *testing.T) (*Service, *fakeQuerier) {
	t.Helper()
	q := newFakeQuerier()
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "test-secret", JWTTTL: time.Hour}}
	issuer := auth.NewIssuer(cfg)
	svc, err := NewService(q, issuer, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return svc, q
}

func TestEnsureSuperadminSkipsWhenUnconfigured(t *testing.T) {
	svc, q := newTestService(t)
	if err := svc.EnsureSuperadmin(context.Background(), "", "", ""); err != nil {
		t.Fatalf("EnsureSuperadmin with no email/password: %v", err)
	}
	if len(q.byEmail) != 0 {
		t.Errorf("no owner should have been created, got %d", len(q.byEmail))
	}
}

func TestEnsureSuperadminCreatesOnce(t *testing.T) {
	svc, q := newTestService(t)
	ctx := context.Background()

	if err := svc.EnsureSuperadmin(ctx, "admin@example.com", "s3cure-pass", "Super Admin"); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	o, ok := q.byEmail["admin@example.com"]
	if !ok {
		t.Fatal("superadmin was not created")
	}
	firstHash := o.PasswordHash

	// A second boot with a different password must not overwrite it: the
	// seed is "create if missing", not "reset on every restart".
	if err := svc.EnsureSuperadmin(ctx, "admin@example.com", "another-pass", "Super Admin"); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if q.byEmail["admin@example.com"].PasswordHash != firstHash {
		t.Error("re-seeding must not change an existing superadmin's password")
	}
	if len(q.byEmail) != 1 {
		t.Errorf("want exactly one owner, got %d", len(q.byEmail))
	}
}
