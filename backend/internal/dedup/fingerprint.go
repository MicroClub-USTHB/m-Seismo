// Package dedup tells whether two reports are the same bug.
//
// A report with a crash gets a fingerprint made from its platform, the
// crash type and the top of its stack trace, with what changes between
// builds and runs taken out: line numbers, memory addresses and offsets.
// Reports with the same fingerprint go to the same issue. The app version
// is left out on purpose, so one issue can show every version a bug
// affects and a bug coming back in a later version is still the same bug.
//
// A report without a crash gets no fingerprint and always opens its own
// issue: what a reporter writes is too loose to group on without merging
// different bugs.
package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// version is part of every fingerprint, so changing how they are made
// can't group old and new reports by accident.
const version = "v1"

// topFrames is how much of the stack trace counts. The frames below are
// mostly the framework calling into the app, the same for many bugs.
const topFrames = 5

var (
	// "12  MyApp  0x0000000104a2c3f4 -[Checkout pay:] + 120" on iOS.
	frameIndex  = regexp.MustCompile(`^\d+\s+`)
	hexNumber   = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	frameOffset = regexp.MustCompile(`\s\+\s*\d+$`)
	// "Checkout.kt:88)" on Android, "Checkout.swift:88:12" elsewhere.
	lineNumber = regexp.MustCompile(`:\d+(:\d+)?\b`)
	number     = regexp.MustCompile(`\d+`)
)

// Fingerprint returns the report's fingerprint, or "" when the report is
// not grouped.
func Fingerprint(r *ingest.Report) string {
	c := r.Crash
	if c == nil || strings.TrimSpace(c.Type) == "" {
		return ""
	}

	parts := []string{version, r.Platform, strings.TrimSpace(c.Type)}
	var frames []string
	for _, f := range c.StackTrace {
		if f = normalizeFrame(f); f != "" {
			frames = append(frames, f)
		}
		if len(frames) == topFrames {
			break
		}
	}
	if len(frames) > 0 {
		parts = append(parts, frames...)
	} else {
		// Without a trace the reason is all there is. Numbers in it are
		// mostly ids and sizes, which differ for the same bug.
		parts = append(parts, number.ReplaceAllString(hexNumber.ReplaceAllString(oneLine(c.Reason), "N"), "N"))
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func normalizeFrame(f string) string {
	f = oneLine(f)
	f = frameIndex.ReplaceAllString(f, "")
	f = frameOffset.ReplaceAllString(f, "")
	f = hexNumber.ReplaceAllString(f, "")
	f = lineNumber.ReplaceAllString(f, "")
	return oneLine(f)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
