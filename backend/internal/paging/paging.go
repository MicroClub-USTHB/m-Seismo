// Package paging implements opaque keyset cursors over (created_at, id).
package paging

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

var ErrInvalidCursor = errors.New("invalid cursor")

// Cursor marks the last row of the previous page.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Page is a request for one page of results.
type Page struct {
	Limit int
	After *Cursor
}

func (c Cursor) Encode() string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func Decode(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	return &Cursor{CreatedAt: t, ID: u}, nil
}

// Normalize clamps the limit to [1, MaxLimit], using DefaultLimit for zero.
func (p Page) Normalize() Page {
	switch {
	case p.Limit <= 0:
		p.Limit = DefaultLimit
	case p.Limit > MaxLimit:
		p.Limit = MaxLimit
	}
	return p
}

// AfterArgs returns the keyset arguments; both are nil on the first page.
func (p Page) AfterArgs() (*time.Time, *uuid.UUID) {
	if p.After == nil {
		return nil, nil
	}
	return &p.After.CreatedAt, &p.After.ID
}

// Result is one page of items plus the cursor for the next page, if any.
type Result[T any] struct {
	Items      []T
	NextCursor string
}

// Trim takes items fetched with Limit+1 rows and builds the result.
func Trim[T any](items []T, limit int, cursorOf func(T) Cursor) Result[T] {
	if len(items) <= limit {
		return Result[T]{Items: items}
	}
	items = items[:limit]
	return Result[T]{Items: items, NextCursor: cursorOf(items[limit-1]).Encode()}
}
