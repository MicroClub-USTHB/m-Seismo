// Package none is the crash.Source used when no crash reporter is
// configured. Reports are processed and published without crash details.
package none

import (
	"context"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash"
)

// Source never finds a crash.
type Source struct{}

var _ crash.Source = Source{}

// Check always succeeds: there is nothing to read.
func (Source) Check(context.Context) error { return nil }

// Find always returns crash.ErrNotFound.
func (Source) Find(context.Context, crash.Query) (crash.Crash, error) {
	return crash.Crash{}, crash.ErrNotFound
}
