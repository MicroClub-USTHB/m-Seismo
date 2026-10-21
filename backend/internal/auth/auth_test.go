package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") {
		t.Error("right password rejected")
	}
	if CheckPassword(h, "wrong horse") {
		t.Error("wrong password accepted")
	}
}

func newIssuer(secret string, ttl time.Duration) *Issuer {
	return NewIssuer(&config.Config{Auth: config.AuthConfig{JWTSecret: secret, JWTTTL: ttl}})
}

func TestTokenRoundTrip(t *testing.T) {
	iss := newIssuer("s3cret", time.Hour)
	owner := uuid.New()

	tok, err := iss.Issue(owner)
	if err != nil {
		t.Fatal(err)
	}
	got, err := iss.Verify(tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if got != owner {
		t.Errorf("Verify = %s, want %s", got, owner)
	}
}

func TestTokenRejected(t *testing.T) {
	iss := newIssuer("s3cret", time.Hour)
	tok, err := iss.Issue(uuid.New())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("other secret", func(t *testing.T) {
		if _, err := newIssuer("other", time.Hour).Verify(tok.AccessToken); err == nil {
			t.Error("token signed with another secret accepted")
		}
	})

	t.Run("expired", func(t *testing.T) {
		later := newIssuer("s3cret", time.Hour)
		later.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		if _, err := later.Verify(tok.AccessToken); err == nil {
			t.Error("expired token accepted")
		}
	})

	t.Run("garbage", func(t *testing.T) {
		if _, err := iss.Verify("not-a-jwt"); err == nil {
			t.Error("garbage accepted")
		}
	})
}
