// Package dedup tells whether two reports are the same bug.
//
// A report with a crash gets a fingerprint made from its platform, the
// crash type and the top frames of the app in its stack trace, with what
// changes between builds and runs taken out: line numbers, memory addresses
// and offsets. Frames of the system, of frameworks and of the SDK are left
// out: they are the same for many bugs. Reports with the same fingerprint go
// to the same issue. The app version is left out on purpose, so one issue
// can show every version a bug affects and a bug coming back in a later
// version is still the same bug.
//
// A stripped build reports every frame as an offset of one symbol, so
// without their offsets different bugs would look the same. A stack without
// function names is therefore grouped on its offsets and the build: they
// mean nothing in another build, so such a bug gets one issue per build. A
// bug hidden in another bug's issue is worse than an issue too many.
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

// topFrames is how many frames of the app count.
const topFrames = 5

// noStackTrace is what the iOS SDK sends for a crash without frames.
const noStackTrace = "(no stack trace available)"

var (
	// "12  MyApp  0x0000000104a2c3f4 -[Checkout pay:] + 120" on iOS.
	frameIndex  = regexp.MustCompile(`^\d+\s+`)
	hexNumber   = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	frameOffset = regexp.MustCompile(`\s\+\s*\d+$`)
	// "Checkout.kt:88)" on Android, "Checkout.swift:88:12" elsewhere.
	lineNumber = regexp.MustCompile(`:\d+(:\d+)?\b`)
	number     = regexp.MustCompile(`\d+`)

	// A frame that isn't the app's: "0   CoreFoundation  0x... ..." on iOS,
	// "at java.util..." on Android, node_modules or native code on the web,
	// and the SDK, which on iOS is built into the app's own binary, so only
	// its symbols tell it apart.
	systemFrame = regexp.MustCompile(`^\d+\s+(CoreFoundation|Foundation|UIKitCore|SwiftUI|SwiftUICore|GraphicsServices|FrontBoardServices|CFNetwork|Combine|libobjc\S*|libsystem\S*|libdispatch\S*|libc\+\+\S*|libswift\S*|dyld)\s` +
		`|^(at\s+)?(java|javax|kotlin|kotlinx|android|androidx|dalvik|libcore|sun|com\.android|com\.google\.android|org\.microclub\.mseismo)\.` +
		`|node_modules/|\[native code\]|\(native\)|\$s7MSeismo|\bCrashReporter\.|\bMSeismo\.|\$sIe|\bthunk for `)

	// "4   MyApp  0x0000000104804e98 MyApp_main + 1556": the module, then the
	// symbol, for a frame with no more than one word as its function.
	symbolFrame = regexp.MustCompile(`^\d+\s+(\S+)\s+0x[0-9a-fA-F]+\s+(\S+)\s+\+\s*\d+$`)
)

// Fingerprint returns the report's fingerprint, or "" when the report is
// not grouped.
func Fingerprint(r *ingest.Report) string {
	c := r.Crash
	if c == nil || strings.TrimSpace(c.Type) == "" {
		return ""
	}

	parts := []string{version, r.Platform, strings.TrimSpace(c.Type)}
	var all, app, offsets []string
	named := false
	for _, f := range c.StackTrace {
		n := normalizeFrame(f)
		if n == "" || n == noStackTrace {
			continue
		}
		all = append(all, n)
		if !systemFrame.MatchString(f) {
			app = append(app, n)
			offsets = append(offsets, withOffset(f))
			named = named || hasName(f)
		}
	}
	switch {
	case len(app) > 0 && !named:
		if r.Release == nil {
			return ""
		}
		parts = append(parts, r.Release.Version, r.Release.Build)
		parts = append(parts, top(offsets)...)
	case len(app) > 0:
		parts = append(parts, top(app)...)
	case len(all) > 0:
		// Nothing but system frames: they are all there is.
		parts = append(parts, top(all)...)
	default:
		// Without a trace the reason is all there is. Numbers in it are
		// mostly ids and sizes, which differ for the same bug.
		parts = append(parts, number.ReplaceAllString(hexNumber.ReplaceAllString(oneLine(c.Reason), "N"), "N"))
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func top(frames []string) []string {
	return frames[:min(len(frames), topFrames)]
}

// hasName reports whether a frame tells which function it is. A stripped
// build gives the module, the entry point or nothing instead of one.
func hasName(f string) bool {
	m := symbolFrame.FindStringSubmatch(oneLine(f))
	if m == nil {
		return true
	}
	symbol := m[2]
	return symbol != m[1] && symbol != "main" && symbol != "<redacted>" &&
		!strings.HasSuffix(symbol, "_main") && !strings.HasPrefix(symbol, "0x")
}

// withOffset is a frame without its index and address. Its offset stays:
// in a stack without names, that is what tells two functions apart.
func withOffset(f string) string {
	return oneLine(hexNumber.ReplaceAllString(frameIndex.ReplaceAllString(oneLine(f), ""), ""))
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
