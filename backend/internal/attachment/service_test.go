package attachment

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type fakeQuerier struct {
	attachments map[uuid.UUID]db.ReportAttachment
}

func (f *fakeQuerier) GetReportAttachment(_ context.Context, id uuid.UUID) (db.ReportAttachment, error) {
	a, ok := f.attachments[id]
	if !ok {
		return db.ReportAttachment{}, pgx.ErrNoRows
	}
	return a, nil
}

func TestSignedURLThenFetch(t *testing.T) {
	id := uuid.New()
	q := &fakeQuerier{attachments: map[uuid.UUID]db.ReportAttachment{
		id: {ID: id, Name: "screenshot", ContentType: "image/png", Data: []byte("fake-png")},
	}}
	svc := NewService(q, NewSigner([]byte("secret")), "https://seismo.example.com", time.Hour)

	link := svc.SignedURL(id)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("SignedURL() = %q, not a URL: %v", link, err)
	}
	if u.Path != "/api/v1/attachments/"+id.String() {
		t.Errorf("path = %q", u.Path)
	}
	expires, err := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
	if err != nil {
		t.Fatalf("expires = %q: %v", u.Query().Get("expires"), err)
	}
	sig := u.Query().Get("sig")

	a, err := svc.Fetch(context.Background(), id, expires, sig)
	if err != nil {
		t.Fatalf("Fetch() err = %v", err)
	}
	if string(a.Data) != "fake-png" || a.ContentType != "image/png" {
		t.Errorf("Fetch() = %+v", a)
	}
}

func TestFetchRejectsExpired(t *testing.T) {
	id := uuid.New()
	q := &fakeQuerier{attachments: map[uuid.UUID]db.ReportAttachment{id: {ID: id}}}
	svc := NewService(q, NewSigner([]byte("secret")), "https://seismo.example.com", time.Hour)

	expires := time.Now().Add(-time.Minute).Unix()
	sig := svc.signer.Sign(id, expires)

	if _, err := svc.Fetch(context.Background(), id, expires, sig); kind(err) != apperr.KindUnauthorized {
		t.Errorf("Fetch() of an expired link: err = %v", err)
	}
}

func TestFetchRejectsBadSignature(t *testing.T) {
	id := uuid.New()
	q := &fakeQuerier{attachments: map[uuid.UUID]db.ReportAttachment{id: {ID: id}}}
	svc := NewService(q, NewSigner([]byte("secret")), "https://seismo.example.com", time.Hour)

	expires := time.Now().Add(time.Hour).Unix()
	if _, err := svc.Fetch(context.Background(), id, expires, "not-the-right-signature"); kind(err) != apperr.KindUnauthorized {
		t.Errorf("Fetch() with a bad signature: err = %v", err)
	}
}

func TestFetchUnknownAttachment(t *testing.T) {
	q := &fakeQuerier{attachments: map[uuid.UUID]db.ReportAttachment{}}
	svc := NewService(q, NewSigner([]byte("secret")), "https://seismo.example.com", time.Hour)

	id := uuid.New()
	expires := time.Now().Add(time.Hour).Unix()
	sig := svc.signer.Sign(id, expires)
	if _, err := svc.Fetch(context.Background(), id, expires, sig); kind(err) != apperr.KindNotFound {
		t.Errorf("Fetch() of an unknown attachment: err = %v", err)
	}
}

func kind(err error) apperr.Kind {
	k, _ := apperr.Classify(err)
	return k
}
