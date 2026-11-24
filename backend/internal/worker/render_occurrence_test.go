package worker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

func TestRenderOccurrence(t *testing.T) {
	r := &ingest.Report{
		Platform:    "android",
		Release:     &ingest.Release{Version: "2.5.0", Build: "500"},
		Device:      &ingest.Device{Model: "Pixel 8", OSName: "Android", OSVersion: "15"},
		UserInput:   &ingest.UserInput{Description: "Again, ping @octocat"},
		Crash:       &ingest.Crash{Type: "java.lang.IllegalStateException", Reason: "Fragment not attached", StackTrace: []string{"com.example.A.b(A.kt:1)"}},
		Breadcrumbs: []ingest.Breadcrumb{{Kind: "navigation", Message: "Opened profile"}},
	}
	id := uuid.New()
	body := renderOccurrence(r, reportInfo{ID: id, InstallationID: uuid.New()}, occurrences{Reports: 12, Installations: 1, Versions: []string{"2.5.0", "2.4.0"}})

	for _, want := range []string{
		"**Seen again**: 12 reports, 1 installation · versions 2.4.0, 2.5.0",
		"Again, ping @\u200boctocat",
		"**java.lang.IllegalStateException**: Fragment not attached",
		"1. Opened profile _(navigation)_",
		"| Device | Pixel 8 |",
		"report `" + id.String() + "`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("comment has no %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "A.kt:1") {
		t.Error("the stack trace, already in the issue, is repeated")
	}

	var many []string
	for i := range 25 {
		many = append(many, fmt.Sprintf("1.%02d", i))
	}
	body = renderOccurrence(r, reportInfo{}, occurrences{Reports: 25, Installations: 25, Versions: many})
	if !strings.Contains(body, "1.24 and 5 more") || strings.Contains(body, "1.04,") {
		t.Errorf("versions not capped to the latest %d:\n%s", maxVersions, body)
	}
}
