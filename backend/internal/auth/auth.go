// Package auth hashes owner passwords and issues the JWTs owners use to
// call the API.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/fx"
	"golang.org/x/crypto/bcrypt"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Module provides the Issuer, also as the httpx.TokenVerifier.
var Module = fx.Module("auth",
	fx.Provide(
		NewIssuer,
		func(i *Issuer) httpx.TokenVerifier { return i },
	),
)

// HashPassword returns the bcrypt hash of password.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword reports whether password matches hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

const issuerName = "m-seismo"

// Issuer signs and verifies owner access tokens (HS256).
type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewIssuer(cfg *config.Config) *Issuer {
	return &Issuer{
		secret: []byte(cfg.Auth.JWTSecret),
		ttl:    cfg.Auth.JWTTTL,
		now:    time.Now,
	}
}

// Token is an issued access token.
type Token struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Issue creates a token for the owner.
func (i *Issuer) Issue(ownerID uuid.UUID) (Token, error) {
	now := i.now()
	exp := now.Add(i.ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    issuerName,
		Subject:   ownerID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return Token{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return Token{AccessToken: signed, TokenType: "Bearer", ExpiresAt: exp}, nil
}

// Verify checks the token's signature, issuer and expiry and returns the
// owner it was issued to.
func (i *Issuer) Verify(token string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuerName),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(i.now),
	)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, errors.New("auth: invalid subject")
	}
	return id, nil
}
