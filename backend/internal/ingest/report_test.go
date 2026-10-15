package ingest

import (
	"encoding/json"
	"strings"
	"testing"
)

const validJSON = `{
	"id": "018f3c2a-7b1e-7000-8000-000000000001",
	"installation_id": "inst-1",
	"mode": "QA",
	"trigger": "shake",
	"occurred_at": "2026-10-15T19:13:00.123Z",
	"title": "ignored, not in the envelope"
}`

func TestValidReport(t *testing.T) {
	var r Report
	if err := json.Unmarshal([]byte(validJSON), &r); err != nil {
		t.Fatal(err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
}

func TestInvalidReport(t *testing.T) {
	cases := map[string]struct {
		field string
		value string
		want  string
	}{
		"v4 id":         {"id", `"6ba7b810-9dad-11d1-80b4-00c04fd430c8"`, "UUIDv7"},
		"zero id":       {"id", `"00000000-0000-0000-0000-000000000000"`, "UUIDv7"},
		"empty install": {"installation_id", `""`, "installation_id"},
		"long install":  {"installation_id", `"` + strings.Repeat("a", 129) + `"`, "installation_id"},
		"bad mode":      {"mode", `"staging"`, "mode"},
		"bad trigger":   {"trigger", `"tap"`, "trigger"},
		"no offset":     {"occurred_at", `"2026-10-15T19:13:00.123+01:00"`, "UTC"},
		"missing time":  {"occurred_at", `"0001-01-01T00:00:00Z"`, "occurred_at"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(validJSON), &m); err != nil {
				t.Fatal(err)
			}
			var v any
			if err := json.Unmarshal([]byte(tc.value), &v); err != nil {
				t.Fatal(err)
			}
			m[tc.field] = v
			raw, _ := json.Marshal(m)

			var r Report
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}
