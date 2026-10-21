// Package apperr defines the error kinds domain services return, so the
// HTTP layer can map them to status codes without services knowing about
// HTTP.
package apperr

import (
	"errors"
	"time"

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
	// KindRateLimited means the caller, or an upstream tracker acting for
	// it, is over a rate limit. Retrying after RetryAfterOf(err) may succeed.
	KindRateLimited
	KindUnavailable
)

// Error is a domain error whose message is safe to show to clients.
type Error struct {
	Kind Kind
	Msg  string
	Err  error
	// Wait is how long to wait before retrying; only meaningful for
	// KindRateLimited.
	Wait time.Duration
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// RetryAfter implements RetryAfterer for rate-limited errors.
func (e *Error) RetryAfter() time.Duration { return e.Wait }

func Unavailable(msg string) error { return &Error{Kind: KindUnavailable, Msg: msg} }

func NotFound(msg string) error     { return &Error{Kind: KindNotFound, Msg: msg} }
func Conflict(msg string) error     { return &Error{Kind: KindConflict, Msg: msg} }
func Invalid(msg string) error      { return &Error{Kind: KindInvalid, Msg: msg} }
func Unauthorized(msg string) error { return &Error{Kind: KindUnauthorized, Msg: msg} }

// RateLimited reports that a limit was hit; retry after wait.
func RateLimited(msg string, wait time.Duration) error {
	return &Error{Kind: KindRateLimited, Msg: msg, Wait: wait}
}

// RetryAfterer is implemented by errors that know when a retry may
// succeed, such as issue.RateLimitError from a tracker.
type RetryAfterer interface {
	RetryAfter() time.Duration
}

// RetryAfterOf returns the retry delay carried by err (anywhere in its
// chain), if any.
func RetryAfterOf(err error) (time.Duration, bool) {
	var ra RetryAfterer
	if errors.As(err, &ra) && ra.RetryAfter() > 0 {
		return ra.RetryAfter(), true
	}
	return 0, false
}

// Postgres error codes we translate.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// Classify returns the kind of err and a client-safe message. Missing rows
// and constraint violations from Postgres are classified too, so services
// can return query errors unchanged, and so is any error carrying a retry
// delay (RetryAfterer), such as a tracker rate limit.
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
	if _, ok := RetryAfterOf(err); ok {
		return KindRateLimited, "rate limited by the issue tracker; retry later"
	}
	return KindInternal, "internal error"
}
