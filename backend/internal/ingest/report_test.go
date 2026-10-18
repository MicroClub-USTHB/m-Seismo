package ingest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specDir is the shared contract the SDKs and the backend follow.
const specDir = "../../../spec"

func loadExample(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(specDir, "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func validate(t *testing.T, m map[string]any) error {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseReport(data)
	if err != nil {
		return err
	}
	return r.Validate()
}

// TestSpecExamples keeps the backend in line with spec/examples: files
// named invalid-* must be rejected, the others accepted.
func TestSpecExamples(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(specDir, "examples", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no spec examples found in %s", specDir)
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			err := validate(t, loadExample(t, name))
			if strings.HasPrefix(name, "invalid-") {
				if err == nil {
					t.Error("accepted, want rejected")
				}
				return
			}
			if err != nil {
				t.Errorf("rejected: %v", err)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name   string
		change func(m map[string]any)
		want   string
	}{
		{"unknown trigger", func(m map[string]any) { m["trigger"] = "foo" }, `trigger: unknown value "foo"`},
		{"timestamp without milliseconds", func(m map[string]any) { m["occurred_at"] = "2026-10-18T14:03:12Z" }, "occurred_at:"},
		{"impossible date", func(m map[string]any) { m["occurred_at"] = "2026-02-30T14:03:12.000Z" }, "occurred_at: is not a valid date"},
		{"bad report id", func(m map[string]any) { m["id"] = "not-a-uuid" }, "id: must be a UUID"},
		{"uuid without dashes", func(m map[string]any) { m["id"] = "0192a5f07c3e7d4a9b1e3f6a2c8d4e51" }, "id:"},
		{"missing installation id", func(m map[string]any) { delete(m, "installation_id") }, "installation_id:"},
		{"missing device", func(m map[string]any) { delete(m, "device") }, "device: is required"},
		{"missing release build", func(m map[string]any) { delete(m["release"].(map[string]any), "build") }, "release.build: is required"},
		{"battery over 1", func(m map[string]any) { m["device"].(map[string]any)["battery_level"] = 1.5 }, "device.battery_level"},
		{"unknown mode", func(m map[string]any) { m["mode"] = "debug" }, `mode: unknown value "debug"`},
		{"crash without details", func(m map[string]any) { m["trigger"] = "non_fatal" }, "crash: is required when trigger is non_fatal"},
		{"title too long", func(m map[string]any) { m["user_input"].(map[string]any)["title"] = strings.Repeat("é", 201) }, "user_input.title: must have at most 200"},
		{"flag of the wrong type", func(m map[string]any) { m["feature_flags"] = map[string]any{"x": []any{1}} }, "feature_flags.x"},
		{"too many logs", func(m map[string]any) {
			logs := make([]any, 501)
			for i := range logs {
				logs[i] = map[string]any{"timestamp": "2026-10-18T14:03:01.004Z", "message": "x"}
			}
			m["logs"] = logs
		}, "logs: has more than 500 items"},
		{"string field given a number", func(m map[string]any) { m["session_id"] = 5 }, "not a valid report"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadExample(t, "shake-report.json")
			tt.change(m)
			err := validate(t, m)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	tests := []struct {
		name   string
		change func(m map[string]any)
	}{
		{"no mode and no consent", func(m map[string]any) { delete(m, "mode"); delete(m, "consent") }},
		{"unknown field", func(m map[string]any) { m["email"] = "someone@example.com" }},
		{"null battery", func(m map[string]any) { m["device"].(map[string]any)["battery_level"] = nil }},
		{"only required fields", func(m map[string]any) {
			for _, k := range []string{"user_input", "feature_flags", "experiments", "logs", "breadcrumbs", "auto_submitted", "bodies_captured", "mode", "consent"} {
				delete(m, k)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadExample(t, "shake-report.json")
			tt.change(m)
			if err := validate(t, m); err != nil {
				t.Errorf("rejected: %v", err)
			}
		})
	}
}

func TestUnsupportedVersion(t *testing.T) {
	m := loadExample(t, "shake-report.json")
	m["schema_version"] = 2
	if err := validate(t, m); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("err = %v, want ErrUnsupportedVersion", err)
	}
}

func TestUnknownFieldsAreNotKept(t *testing.T) {
	m := loadExample(t, "shake-report.json")
	m["email"] = "someone@example.com"
	data, _ := json.Marshal(m)
	r, err := ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(r)
	if strings.Contains(string(out), "someone@example.com") {
		t.Error("an unknown field survived decoding and would be stored")
	}
}

func TestNormalizeDropsBodiesUnlessCaptured(t *testing.T) {
	r := &Report{NetworkRequests: []NetworkRequest{{RequestBody: "secret", ResponseBody: "secret"}}}
	r.Normalize()
	if r.NetworkRequests[0].RequestBody != "" || r.NetworkRequests[0].ResponseBody != "" {
		t.Error("bodies kept although bodies_captured is false")
	}

	r = &Report{BodiesCaptured: true, NetworkRequests: []NetworkRequest{{RequestBody: "ok"}}}
	r.Normalize()
	if r.NetworkRequests[0].RequestBody != "ok" {
		t.Error("body dropped although bodies_captured is true")
	}
}

func TestNormalizeLowercasesUUIDs(t *testing.T) {
	m := loadExample(t, "shake-report.json")
	m["id"] = "0192A5F0-7C3E-7D4A-9B1E-3F6A2C8D4E51"
	m["installation_id"] = "5B2E0F6C-1A3D-4E5F-8A7B-9C0D1E2F3A4B"
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate with uppercase UUIDs (as iOS sends them): %v", err)
	}
	r.Normalize()
	if r.ID != "0192a5f0-7c3e-7d4a-9b1e-3f6a2c8d4e51" || r.InstallationID != "5b2e0f6c-1a3d-4e5f-8a7b-9c0d1e2f3a4b" {
		t.Errorf("after Normalize: id %q, installation_id %q, want lowercase", r.ID, r.InstallationID)
	}
}
