package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
)

type fakeQuerier struct {
	db.Querier
	app  db.App
	keys []db.AppKey
}

func (f *fakeQuerier) GetApp(_ context.Context, arg db.GetAppParams) (db.App, error) {
	if arg.ID != f.app.ID || arg.OwnerID != f.app.OwnerID {
		return db.App{}, pgx.ErrNoRows
	}
	return f.app, nil
}

func (f *fakeQuerier) CreateAppKey(_ context.Context, arg db.CreateAppKeyParams) (db.AppKey, error) {
	k := db.AppKey{ID: uuid.New(), AppID: arg.AppID, Kind: arg.Kind, KeyHash: arg.KeyHash, KeyPrefix: arg.KeyPrefix}
	f.keys = append(f.keys, k)
	return k, nil
}

func (f *fakeQuerier) GetActiveAppKeyByHash(_ context.Context, arg db.GetActiveAppKeyByHashParams) (db.AppKey, error) {
	for _, k := range f.keys {
		if bytes.Equal(k.KeyHash, arg.KeyHash) && k.Kind == arg.Kind && k.RevokedAt == nil {
			return k, nil
		}
	}
	return db.AppKey{}, pgx.ErrNoRows
}

func TestKeyLifecycle(t *testing.T) {
	owner := uuid.New()
	q := &fakeQuerier{app: db.App{ID: uuid.New(), OwnerID: owner}}
	svc := NewService(q, nil, nil, &config.Config{})
	ctx := context.Background()

	k, err := svc.CreateKey(ctx, owner, q.app.ID, db.AppKeyKindSdk)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Secret, "msk_") || !strings.HasPrefix(k.Secret, k.Prefix) || len(k.Prefix) != len("msk_")+shownSecretLen {
		t.Errorf("key = %+v", k)
	}
	if bytes.Contains(q.keys[0].KeyHash, []byte(k.Secret)) || len(q.keys[0].KeyHash) != 32 {
		t.Error("stored key must be a sha256 hash, not the secret")
	}

	appID, err := svc.Authenticate(ctx, k.Secret)
	if err != nil || appID != q.app.ID {
		t.Errorf("Authenticate = %s, %v", appID, err)
	}

	for _, bad := range []string{"", "msk_nope", "other_" + k.Secret} {
		if _, err := svc.Authenticate(ctx, bad); kind(err) != apperr.KindUnauthorized {
			t.Errorf("Authenticate(%q): err = %v", bad, err)
		}
	}

	if _, err := svc.CreateKey(ctx, uuid.New(), q.app.ID, db.AppKeyKindSdk); kind(err) != apperr.KindNotFound {
		t.Errorf("another owner created a key: err = %v", err)
	}
}

func TestKeyKindsAreSeparate(t *testing.T) {
	owner := uuid.New()
	q := &fakeQuerier{app: db.App{ID: uuid.New(), OwnerID: owner}}
	svc := NewService(q, nil, nil, &config.Config{})
	ctx := context.Background()

	sdk, err := svc.CreateKey(ctx, owner, q.app.ID, db.AppKeyKindSdk)
	if err != nil {
		t.Fatal(err)
	}
	co, err := svc.CreateKey(ctx, owner, q.app.ID, db.AppKeyKindCoordinator)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(co.Secret, "mco_") {
		t.Errorf("coordinator key = %q", co.Secret)
	}

	if appID, err := svc.AuthenticateCoordinator(ctx, co.Secret); err != nil || appID != q.app.ID {
		t.Errorf("AuthenticateCoordinator = %s, %v", appID, err)
	}
	if _, err := svc.AuthenticateCoordinator(ctx, sdk.Secret); kind(err) != apperr.KindUnauthorized {
		t.Errorf("SDK key accepted as a coordinator key: err = %v", err)
	}
	if _, err := svc.Authenticate(ctx, co.Secret); kind(err) != apperr.KindUnauthorized {
		t.Errorf("coordinator key accepted as an SDK key: err = %v", err)
	}
	// A coordinator key renamed to look like an SDK key is still refused.
	if _, err := svc.Authenticate(ctx, "msk_"+strings.TrimPrefix(co.Secret, "mco_")); kind(err) != apperr.KindUnauthorized {
		t.Errorf("renamed coordinator key accepted: err = %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	svc := NewService(&fakeQuerier{}, nil, nil, &config.Config{})
	_, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "App", Platform: "windows", BundleID: "x"})
	if kind(err) != apperr.KindInvalid {
		t.Errorf("err = %v", err)
	}
}

func kind(err error) apperr.Kind {
	k, _ := apperr.Classify(err)
	return k
}

type denyLimiter struct{ wait time.Duration }

func (d denyLimiter) Allow(context.Context, string, ratelimit.Limit) (ratelimit.Result, error) {
	return ratelimit.Result{RetryAfter: d.wait}, nil
}

func TestTestIssueIsThrottled(t *testing.T) {
	owner := uuid.New()
	q := &fakeQuerier{app: db.App{ID: uuid.New(), OwnerID: owner}}
	svc := NewService(q, nil, denyLimiter{wait: 20 * time.Second}, &config.Config{Limits: config.LimitsConfig{TestIssuesPerMinute: 1}})

	_, err := svc.SendTestIssue(context.Background(), owner, q.app.ID, TestIssueInput{})
	if kind(err) != apperr.KindRateLimited {
		t.Fatalf("err = %v, want rate limited", err)
	}
	if wait, ok := apperr.RetryAfterOf(err); !ok || wait != 20*time.Second {
		t.Errorf("retry after = %v, %v", wait, ok)
	}
}
