package crashlytics

import (
	"fmt"
	"strings"

	fc "google.golang.org/api/firebasecrashlytics/v1alpha"
)

// formatTrace renders the event's crash trace as plain text for the issue.
//
// Android and web events carry exceptions, last thrown first, so the first
// one is printed as the crash and the rest as its causes. iOS events carry
// threads (crashes) or errors (non-fatals); only the thread or error that
// Crashlytics blamed is printed, to keep the issue readable.
func formatTrace(ev *fc.Event) string {
	var b strings.Builder

	for i, ex := range ev.Exceptions {
		if i > 0 {
			b.WriteString("Caused by: ")
		}
		fmt.Fprintf(&b, "%s: %s\n", ex.Type, ex.ExceptionMessage)
		writeFrames(&b, ex.Frames)
	}

	if e := blamedError(ev.Errors); e != nil {
		fmt.Fprintf(&b, "%s: %s\n", e.Title, e.Subtitle)
		writeFrames(&b, e.Frames)
	}

	if t := blamedThread(ev.Threads); t != nil {
		name := t.Name
		if name == "" {
			name = t.Title
		}
		fmt.Fprintf(&b, "Thread %s", name)
		if t.Signal != "" {
			fmt.Fprintf(&b, " (%s %s)", t.Signal, t.SignalCode)
		}
		b.WriteString("\n")
		writeFrames(&b, t.Frames)
	}

	return b.String()
}

func writeFrames(b *strings.Builder, frames []*fc.Frame) {
	for _, f := range frames {
		fmt.Fprintf(b, "    at %s", f.Symbol)
		switch {
		case f.File != "" && f.Line > 0:
			fmt.Fprintf(b, " (%s:%d)", f.File, f.Line)
		case f.File != "":
			fmt.Fprintf(b, " (%s)", f.File)
		case f.Library != "":
			fmt.Fprintf(b, " (%s)", f.Library)
		}
		b.WriteString("\n")
	}
}

// blamedError returns the blamed error, or the first one if none is blamed.
func blamedError(errs []*fc.Error) *fc.Error {
	for _, e := range errs {
		if e.Blamed {
			return e
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// blamedThread returns the blamed thread, else the crashed one, else nil.
func blamedThread(threads []*fc.Thread) *fc.Thread {
	var crashed *fc.Thread
	for _, t := range threads {
		if t.Blamed {
			return t
		}
		if t.Crashed && crashed == nil {
			crashed = t
		}
	}
	return crashed
}
