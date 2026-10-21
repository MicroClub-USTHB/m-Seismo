package ingest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSentryReferencesForMobileAndWeb(t *testing.T) {
	for _, platform := range []string{"ios", "android", "flutter", "react_native", "web"} {
		t.Run(platform, func(t *testing.T) {
			m := loadExample(t, "shake-report.json")
			m["platform"] = platform
			m["trigger"] = "crash"
			m["sentry"] = map[string]any{"event_id": strings.Repeat("A", 32), "auth_token": "must-not-be-stored"}
			if err := validate(t, m); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(m)
			report, err := ParseReport(data)
			if err != nil {
				t.Fatal(err)
			}
			report.Normalize()
			encoded, _ := json.Marshal(report)
			if report.Sentry.EventID != strings.Repeat("a", 32) || strings.Contains(string(encoded), "must-not-be-stored") {
				t.Fatal("reference not normalized or unknown credential stored")
			}
		})
	}
}

func TestSentryRejectsMissingMalformedAndPathEventIDs(t *testing.T) {
	for _, id := range []string{"", "../event", strings.Repeat("g", 32), strings.Repeat("a", 31)} {
		m := loadExample(t, "shake-report.json")
		m["sentry"] = map[string]any{"event_id": id}
		if err := validate(t, m); err == nil {
			t.Errorf("accepted %q", id)
		}
	}
}
