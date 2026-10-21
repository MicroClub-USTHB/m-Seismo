package httpx

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
)

// TokenVerifier validates a bearer token and returns the owner it was
// issued to. Implemented by auth.Issuer.
type TokenVerifier interface {
	Verify(token string) (uuid.UUID, error)
}

// AuthMiddleware rejects requests without a valid owner bearer token.
type AuthMiddleware func(http.Handler) http.Handler

type ownerIDKey struct{}

// NewAuthMiddleware builds the middleware that protects owner routes.
func NewAuthMiddleware(v TokenVerifier) AuthMiddleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				Error(w, r, apperr.Unauthorized("missing bearer token"))
				return
			}
			ownerID, err := v.Verify(token)
			if err != nil {
				Error(w, r, apperr.Unauthorized("invalid or expired token"))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithOwnerID(r.Context(), ownerID)))
		})
	}
}

// WithOwnerID returns a context carrying the authenticated owner.
func WithOwnerID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ownerIDKey{}, id)
}

// OwnerID returns the authenticated owner. It must only be called behind
// AuthMiddleware.
func OwnerID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(ownerIDKey{}).(uuid.UUID)
	return id
}
