package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
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
	k := db.AppKey{ID: uuid.New(), AppID: arg.AppID, KeyHash: arg.KeyHash, KeyPrefix: arg.KeyPrefix}
	f.keys = append(f.keys, k)
	return k, nil
}

func (f *fakeQuerier) GetActiveAppKeyByHash(_ context.Context, hash []byte) (db.AppKey, error) {
	for _, k := range f.keys {
		if bytes.Equal(k.KeyHash, hash) && k.RevokedAt == nil {
			return k, nil
		}
	}
	return db.AppKey{}, pgx.ErrNoRows
}

func TestKeyLifecycle(t *testing.T) {
	owner := uuid.New()
	q := &fakeQuerier{app: db.App{ID: uuid.New(), OwnerID: owner}}
	svc := NewService(q)
	ctx := context.Background()

	k, err := svc.CreateKey(ctx, owner, q.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Secret, keyPrefix) || !strings.HasPrefix(k.Secret, k.Prefix) || len(k.Prefix) != shownPrefixLen {
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

	if _, err := svc.CreateKey(ctx, uuid.New(), q.app.ID); kind(err) != apperr.KindNotFound {
		t.Errorf("another owner created a key: err = %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	svc := NewService(&fakeQuerier{})
	_, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "App", Platform: "windows", BundleID: "x"})
	if kind(err) != apperr.KindInvalid {
		t.Errorf("err = %v", err)
	}
}

func kind(err error) apperr.Kind {
	k, _ := apperr.Classify(err)
	return k
}
