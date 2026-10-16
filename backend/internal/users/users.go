// Package users manages the accounts that own projects and git connections.
package users

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/audit"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/opt"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/paging"
)

const entityType = "user"

type User struct {
	ID        uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	Email     *string   `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name  string  `json:"name"`
	Email *string `json:"email"`
}

type UpdateInput struct {
	Name  opt.Field[string] `json:"name"`
	Email opt.Field[string] `json:"email"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

const columns = `user_id, name, email, created_at, updated_at`

func scan(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (s *Service) Create(ctx context.Context, in CreateInput) (User, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = normalizeEmail(in.Email)
	var v apperr.Validator
	validateName(&v, in.Name)
	validateEmail(&v, in.Email)
	if err := v.Err(); err != nil {
		return User{}, err
	}

	var u User
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		u, err = scan(tx.QueryRow(ctx,
			`INSERT INTO users (name, email) VALUES ($1, $2) RETURNING `+columns,
			in.Name, in.Email))
		if err != nil {
			return mapErr(err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &u.ID, Action: "user.create", EntityType: entityType, EntityID: u.ID, Payload: u,
		})
	})
	return u, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := scan(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM users WHERE user_id = $1`, id))
	if err != nil {
		return User{}, mapErr(err)
	}
	return u, nil
}

func (s *Service) List(ctx context.Context, page paging.Page) (paging.Result[User], error) {
	page = page.Normalize()
	afterTS, afterID := page.AfterArgs()
	rows, err := s.pool.Query(ctx, `
		SELECT `+columns+` FROM users
		WHERE $1::timestamptz IS NULL OR (created_at, user_id) > ($1, $2)
		ORDER BY created_at, user_id
		LIMIT $3`, afterTS, afterID, page.Limit+1)
	if err != nil {
		return paging.Result[User]{}, fmt.Errorf("users: list: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (User, error) { return scan(r) })
	if err != nil {
		return paging.Result[User]{}, fmt.Errorf("users: list: %w", err)
	}
	return paging.Trim(items, page.Limit, func(u User) paging.Cursor {
		return paging.Cursor{CreatedAt: u.CreatedAt, ID: u.ID}
	}), nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (User, error) {
	var v apperr.Validator
	if in.Name.Set {
		in.Name.Value = strings.TrimSpace(in.Name.Value)
		v.Check(!in.Name.Null, "name", "must not be null")
		if !in.Name.Null {
			validateName(&v, in.Name.Value)
		}
	}
	if in.Email.Set {
		in.Email.Value = strings.TrimSpace(in.Email.Value)
		if !in.Email.Null {
			validateEmail(&v, &in.Email.Value)
		}
	}
	if err := v.Err(); err != nil {
		return User{}, err
	}

	email := normalizeEmail(in.Email.Ptr())
	changes := map[string]any{}
	in.Name.PutIfSet(changes, "name")
	if in.Email.Set {
		changes["email"] = email
	}

	var u User
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		u, err = scan(tx.QueryRow(ctx, `
			UPDATE users SET
				name  = CASE WHEN $2 THEN $3 ELSE name END,
				email = CASE WHEN $4 THEN $5 ELSE email END
			WHERE user_id = $1
			RETURNING `+columns,
			id, in.Name.Set, in.Name.Value, in.Email.Set, email))
		if err != nil {
			return mapErr(err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			UserID: &u.ID, Action: "user.update", EntityType: entityType, EntityID: u.ID, Payload: changes,
		})
	})
	return u, err
}

// Delete removes the user and, through ON DELETE CASCADE, everything it owns.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, id)
		if err != nil {
			return fmt.Errorf("users: delete: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound(entityType)
		}
		// user_id stays NULL: the row it would reference is gone.
		return audit.Record(ctx, tx, audit.Entry{
			Action: "user.delete", EntityType: entityType, EntityID: id,
		})
	})
}

func normalizeEmail(e *string) *string {
	if e == nil {
		return nil
	}
	s := strings.TrimSpace(*e)
	if s == "" {
		return nil
	}
	return &s
}

func validateName(v *apperr.Validator, name string) {
	n := utf8.RuneCountInString(name)
	v.Check(n >= 1 && n <= 255, "name", "must be between 1 and 255 characters")
}

func validateEmail(v *apperr.Validator, email *string) {
	if email == nil || *email == "" {
		return
	}
	addr, err := mail.ParseAddress(*email)
	v.Check(err == nil && addr.Address == *email && len(*email) <= 320, "email", "must be a valid email address")
}

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound(entityType)
	}
	if code, constraint, ok := db.PgError(err); ok && code == db.CodeUniqueViolation && constraint == "users_email_key" {
		return apperr.Conflict("email is already in use")
	}
	return fmt.Errorf("users: %w", err)
}
