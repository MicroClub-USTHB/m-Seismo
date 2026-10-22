// Package owner manages owner accounts: registration, login and the
// owner's own profile. Owners own projects, apps and provider connections.
package owner

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/auth"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

const (
	minPasswordLen = 8
	// bcrypt ignores everything past 72 bytes.
	maxPasswordLen = 72
	maxNameLen     = 200
)

// Owner is the public view of an owner account.
type Owner struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toOwner(o db.Owner) Owner {
	return Owner{ID: o.ID, Email: o.Email, Name: o.Name, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

type Service struct {
	q      db.Querier
	issuer *auth.Issuer
	// dummyHash is compared against when an email is unknown, so login takes
	// the same time whether or not the account exists.
	dummyHash string
}

func NewService(q db.Querier, issuer *auth.Issuer) (*Service, error) {
	h, err := auth.HashPassword("m-seismo-timing-equaliser")
	if err != nil {
		return nil, err
	}
	return &Service{q: q, issuer: issuer, dummyHash: h}, nil
}

type RegisterInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// Register creates an owner and returns it with a fresh access token.
func (s *Service) Register(ctx context.Context, in RegisterInput) (Owner, auth.Token, error) {
	email, err := normaliseEmail(in.Email)
	if err != nil {
		return Owner{}, auth.Token{}, err
	}
	name, err := validateName(in.Name)
	if err != nil {
		return Owner{}, auth.Token{}, err
	}
	if err := validatePassword(in.Password); err != nil {
		return Owner{}, auth.Token{}, err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return Owner{}, auth.Token{}, err
	}

	o, err := s.q.CreateOwner(ctx, db.CreateOwnerParams{Email: email, PasswordHash: hash, Name: name})
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindConflict {
			return Owner{}, auth.Token{}, apperr.Conflict("an account with this email already exists")
		}
		return Owner{}, auth.Token{}, err
	}
	return s.withToken(o)
}

// Login checks the credentials and returns the owner with a fresh token.
func (s *Service) Login(ctx context.Context, email, password string) (Owner, auth.Token, error) {
	invalid := apperr.Unauthorized("invalid email or password")

	o, err := s.q.GetOwnerByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		auth.CheckPassword(s.dummyHash, password)
		return Owner{}, auth.Token{}, invalid
	}
	if err != nil {
		return Owner{}, auth.Token{}, err
	}
	if !auth.CheckPassword(o.PasswordHash, password) {
		return Owner{}, auth.Token{}, invalid
	}
	return s.withToken(o)
}

func (s *Service) withToken(o db.Owner) (Owner, auth.Token, error) {
	tok, err := s.issuer.Issue(o.ID)
	if err != nil {
		return Owner{}, auth.Token{}, err
	}
	return toOwner(o), tok, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Owner, error) {
	o, err := s.q.GetOwnerByID(ctx, id)
	if err != nil {
		return Owner{}, err
	}
	return toOwner(o), nil
}

// UpdateInput changes only the fields that are set. Changing the email or
// the password requires the current password.
type UpdateInput struct {
	Email           *string `json:"email"`
	Name            *string `json:"name"`
	Password        *string `json:"password"`
	CurrentPassword *string `json:"current_password"`
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (Owner, error) {
	o, err := s.q.GetOwnerByID(ctx, id)
	if err != nil {
		return Owner{}, err
	}

	params := db.UpdateOwnerParams{ID: o.ID, Email: o.Email, PasswordHash: o.PasswordHash, Name: o.Name}

	if in.Email != nil || in.Password != nil {
		if in.CurrentPassword == nil || !auth.CheckPassword(o.PasswordHash, *in.CurrentPassword) {
			return Owner{}, apperr.Unauthorized("current_password is required and must be correct")
		}
	}
	if in.Email != nil {
		if params.Email, err = normaliseEmail(*in.Email); err != nil {
			return Owner{}, err
		}
	}
	if in.Name != nil {
		if params.Name, err = validateName(*in.Name); err != nil {
			return Owner{}, err
		}
	}
	if in.Password != nil {
		if err := validatePassword(*in.Password); err != nil {
			return Owner{}, err
		}
		if params.PasswordHash, err = auth.HashPassword(*in.Password); err != nil {
			return Owner{}, err
		}
	}

	updated, err := s.q.UpdateOwner(ctx, params)
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindConflict {
			return Owner{}, apperr.Conflict("an account with this email already exists")
		}
		return Owner{}, err
	}
	return toOwner(updated), nil
}

// EnsureSuperadmin seeds one owner from config.SuperadminConfig, so a fresh
// deployment has someone able to log in and create other owners. It is a
// no-op when email or password is empty, and when an owner with that email
// already exists — it never overwrites an existing password, so changing
// it after the first boot (or rotating SEISMO_SUPERADMIN_PASSWORD) does not
// reset it back on the next restart.
func (s *Service) EnsureSuperadmin(ctx context.Context, email, password, name string) error {
	if email == "" || password == "" {
		return nil
	}
	if _, _, err := s.Register(ctx, RegisterInput{Email: email, Password: password, Name: name}); err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindConflict {
			return nil
		}
		return err
	}
	return nil
}

// Delete removes the owner and, by cascade, everything they own.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := s.q.DeleteOwner(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("owner not found")
	}
	return nil
}

func normaliseEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", apperr.Invalid("email is not a valid address")
	}
	return email, nil
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > maxNameLen {
		return "", apperr.Invalid("name is required and must be at most 200 characters")
	}
	return name, nil
}

func validatePassword(p string) error {
	if len(p) < minPasswordLen || len(p) > maxPasswordLen {
		return apperr.Invalid("password must be between 8 and 72 bytes")
	}
	return nil
}
