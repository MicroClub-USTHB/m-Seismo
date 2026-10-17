// Package apperr defines the error kinds domain services return, so the
// HTTP layer can map them to status codes without services knowing about
// HTTP.
package apperr

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Kind int

const (
	KindInternal Kind = iota
	KindNotFound
	KindConflict
	KindInvalid
	KindUnauthorized
)

// Error is a domain error whose message is safe to show to clients.
type Error struct {
	Kind Kind
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

func NotFound(msg string) error     { return &Error{Kind: KindNotFound, Msg: msg} }
func Conflict(msg string) error     { return &Error{Kind: KindConflict, Msg: msg} }
func Invalid(msg string) error      { return &Error{Kind: KindInvalid, Msg: msg} }
func Unauthorized(msg string) error { return &Error{Kind: KindUnauthorized, Msg: msg} }

// Postgres error codes we translate.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// Classify returns the kind of err and a client-safe message. Missing rows
// and constraint violations from Postgres are classified too, so services
// can return query errors unchanged.
func Classify(err error) (Kind, string) {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Kind, ae.Msg
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return KindNotFound, "not found"
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolation:
			return KindConflict, "already exists"
		case pgForeignKeyViolation:
			return KindInvalid, "references a resource that does not exist"
		}
	}
	return KindInternal, "internal error"
}
