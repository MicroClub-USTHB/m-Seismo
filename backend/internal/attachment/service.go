package attachment

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// Querier loads a stored attachment. Implemented by db.Querier.
type Querier interface {
	GetReportAttachment(ctx context.Context, id uuid.UUID) (db.ReportAttachment, error)
}

// Attachment is a stored file, ready to be served.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

type Service struct {
	q       Querier
	signer  *Signer
	baseURL string
	ttl     time.Duration
}

func NewService(q Querier, signer *Signer, baseURL string, ttl time.Duration) *Service {
	return &Service{q: q, signer: signer, baseURL: strings.TrimRight(baseURL, "/"), ttl: ttl}
}

// SignedURL builds the private, expiring link an issue shows for id.
func (s *Service) SignedURL(id uuid.UUID) string {
	expires := time.Now().Add(s.ttl).Unix()
	sig := s.signer.Sign(id, expires)
	v := url.Values{"expires": {strconv.FormatInt(expires, 10)}, "sig": {sig}}
	return fmt.Sprintf("%s/api/v1/attachments/%s?%s", s.baseURL, id, v.Encode())
}

// Fetch loads the attachment id, once expires and sig check out against
// what SignedURL would have produced for it.
func (s *Service) Fetch(ctx context.Context, id uuid.UUID, expires int64, sig string) (Attachment, error) {
	if time.Now().Unix() > expires || !s.signer.Verify(id, expires, sig) {
		return Attachment{}, apperr.Unauthorized("invalid or expired link")
	}
	a, err := s.q.GetReportAttachment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, apperr.NotFound("attachment not found")
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("attachment: load %s: %w", id, err)
	}
	return Attachment{Name: a.Name, ContentType: a.ContentType, Data: a.Data}, nil
}
