package crashprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/google/uuid"
)

type rotationDB struct {
	db.Querier
	rows       []db.ListAppCrashProviderCredentialsRow
	concurrent bool
}

func (q *rotationDB) ListAppCrashProviderCredentials(context.Context) ([]db.ListAppCrashProviderCredentialsRow, error) {
	return q.rows, nil
}
func (q *rotationDB) RotateAppCrashProviderCredentials(_ context.Context, p db.RotateAppCrashProviderCredentialsParams) (int64, error) {
	if q.concurrent {
		return 0, nil
	}
	for i, row := range q.rows {
		if row.AppID == p.AppID && bytes.Equal(row.Credentials, p.OldCredentials) {
			q.rows[i].Credentials = p.NewCredentials
			return 1, nil
		}
	}
	return 0, nil
}
func TestEncryptionKeyRotationRetainsLegacyTokensAndPreservesAppBinding(t *testing.T) {
	oldKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	newKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	old, err := newVault(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	current, err := newVault(newKey, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	app := uuid.New()
	encrypted, err := old.seal(app, "private-token")
	if err != nil {
		t.Fatal(err)
	}
	if token, err := current.open(app, encrypted); err != nil || token != "private-token" {
		t.Fatalf("legacy token unavailable: %v", err)
	}
	if _, err := current.open(uuid.New(), encrypted); err == nil {
		t.Fatal("rotation lost app binding")
	}
	q := &rotationDB{rows: []db.ListAppCrashProviderCredentialsRow{{AppID: app, Credentials: encrypted}}}
	s := &Service{q: q, vault: current}
	if count, err := s.RotateCredentials(context.Background()); err != nil || count != 1 {
		t.Fatalf("rotation: %d %v", count, err)
	}
	withoutOld, err := newVault(newKey)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := withoutOld.open(app, q.rows[0].Credentials); err != nil || token != "private-token" {
		t.Fatalf("previous key cannot be removed: %v", err)
	}
	if _, err := old.open(app, q.rows[0].Credentials); err == nil {
		t.Fatal("token still encrypted with retired key")
	}
	// Rerunning after a partial deployment is safe.
	if count, err := s.RotateCredentials(context.Background()); err != nil || count != 1 {
		t.Fatalf("repeat rotation: %d %v", count, err)
	}
	q.concurrent = true
	before := append([]byte(nil), q.rows[0].Credentials...)
	if _, err := s.RotateCredentials(context.Background()); err == nil {
		t.Fatal("concurrent replacement silently overwritten")
	}
	if !bytes.Equal(before, q.rows[0].Credentials) {
		t.Fatal("rotation overwrote concurrent credentials")
	}
	if _, err := newVault("", oldKey); err == nil {
		t.Fatal("previous keys accepted without active key")
	}
	if _, err := newVault(newKey, "invalid"); err == nil {
		t.Fatal("invalid previous key accepted")
	}
}
