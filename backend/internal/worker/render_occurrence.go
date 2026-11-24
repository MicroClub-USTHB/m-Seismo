package worker

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// maxVersions is how many affected versions a comment lists.
const maxVersions = 20

// occurrences counts the reports grouped under one issue, the new one
// included.
type occurrences struct {
	Reports       int
	Installations int
	Versions      []string
}

// renderOccurrence is the comment added to an open issue when another
// report of the same bug comes in: the counts so far, then what is new in
// this report. The crash itself is the same as in the issue, so it is left
// out.
func renderOccurrence(r *ingest.Report, info reportInfo, n occurrences) string {
	var b strings.Builder

	fmt.Fprintf(&b, "**Seen again**: %s, %s", plural(n.Reports, "report"), plural(n.Installations, "installation"))
	if len(n.Versions) > 0 {
		versions := slices.Clone(n.Versions)
		slices.Sort(versions)
		more := ""
		if len(versions) > maxVersions {
			more = fmt.Sprintf(" and %d more", len(versions)-maxVersions)
			versions = versions[len(versions)-maxVersions:]
		}
		fmt.Fprintf(&b, " · versions %s%s", text(strings.Join(versions, ", ")), more)
	}
	b.WriteString("\n\n")

	if in := r.UserInput; in != nil && in.Description != "" {
		b.WriteString(text(in.Description))
		b.WriteString("\n\n")
	}
	if c := r.Crash; c != nil && c.Reason != "" {
		fmt.Fprintf(&b, "**%s**: %s\n\n", text(c.Type), text(c.Reason))
	}
	b.WriteString(stepsSection(r))
	b.WriteString(environmentSection(r))
	renderServerLogs(&b, info.ServerLogs)
	renderHealth(&b, info.Health)
	if info.ScreenshotURL != "" {
		fmt.Fprintf(&b, "[Screenshot](%s) _(link expires; only people with access to this issue should open it)_\n\n", info.ScreenshotURL)
	}

	b.WriteString("---\n")
	fmt.Fprintf(&b, "<sub>Added by m-Seismo · report `%s` · installation `%s`</sub>\n", info.ID, info.InstallationID)
	return truncate(b.String(), maxBody, "\n\n_(truncated)_")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
