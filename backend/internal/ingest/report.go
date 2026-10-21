// Package ingest receives reports from the SDKs: it checks the app key and
// the rate limits, validates the report against spec/report.schema.json,
// stores it once, tells the pipeline about it and answers 202.
package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// SchemaVersion is the report schema version this backend accepts.
const SchemaVersion = 1

// ErrUnsupportedVersion is returned for a schema_version other than
// SchemaVersion.
var ErrUnsupportedVersion = errors.New("unsupported schema_version")

// Report is one report as described by spec/report.schema.json. Fields the
// schema doesn't know are dropped when decoding, so they are never stored.
type Report struct {
	SchemaVersion   int                `json:"schema_version"`
	ID              string             `json:"id"`
	InstallationID  string             `json:"installation_id"`
	SessionID       string             `json:"session_id"`
	OccurredAt      string             `json:"occurred_at"`
	Platform        string             `json:"platform"`
	SDK             *SDK               `json:"sdk"`
	Environment     string             `json:"environment"`
	Release         *Release           `json:"release"`
	Mode            string             `json:"mode,omitempty"`
	Trigger         string             `json:"trigger"`
	AutoSubmitted   bool               `json:"auto_submitted"`
	Consent         string             `json:"consent,omitempty"`
	UserInput       *UserInput         `json:"user_input,omitempty"`
	Device          *Device            `json:"device"`
	FeatureFlags    map[string]any     `json:"feature_flags,omitempty"`
	Experiments     map[string]string  `json:"experiments,omitempty"`
	Logs            []LogLine          `json:"logs,omitempty"`
	Breadcrumbs     []Breadcrumb       `json:"breadcrumbs,omitempty"`
	Crash           *Crash             `json:"crash,omitempty"`
	NetworkRequests []NetworkRequest   `json:"network_requests,omitempty"`
	BodiesCaptured  bool               `json:"bodies_captured"`
	Crashlytics     *CrashlyticsFields `json:"crashlytics,omitempty"`
}

type SDK struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Release struct {
	Version string `json:"version"`
	Build   string `json:"build"`
}

type UserInput struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Severity    string `json:"severity,omitempty"`
}

type Device struct {
	OSName               string   `json:"os_name"`
	OSVersion            string   `json:"os_version"`
	Model                string   `json:"model"`
	Locale               string   `json:"locale,omitempty"`
	BatteryLevel         *float64 `json:"battery_level,omitempty"`
	AvailableMemoryBytes *int64   `json:"available_memory_bytes,omitempty"`
	NetworkType          string   `json:"network_type,omitempty"`
	Orientation          string   `json:"orientation,omitempty"`
}

type LogLine struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level,omitempty"`
	Message   string `json:"message"`
}

type Breadcrumb struct {
	Timestamp string `json:"timestamp"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
}

type Crash struct {
	Type       string   `json:"type"`
	Reason     string   `json:"reason,omitempty"`
	StackTrace []string `json:"stack_trace"`
}

type NetworkRequest struct {
	Timestamp    string `json:"timestamp"`
	URL          string `json:"url"`
	Method       string `json:"method"`
	StatusCode   *int   `json:"status_code,omitempty"`
	DurationMS   *int64 `json:"duration_ms"`
	RequestBody  string `json:"request_body,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
}

type CrashlyticsFields struct {
	InstallationID string `json:"installation_id,omitempty"`
}

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

	platforms    = set("ios", "android", "flutter", "react_native")
	modes        = set("internal", "qa", "production")
	triggers     = set("shake", "screenshot", "long_press", "manual", "crash", "non_fatal")
	consents     = set("given", "not_required")
	categories   = set("bug", "crash", "ui", "other")
	severities   = set("low", "medium", "high", "critical")
	networkTypes = set("wifi", "cellular", "ethernet", "none", "unknown")
	orientations = set("portrait", "landscape", "unknown")
	logLevels    = set("debug", "info", "warning", "error")
	crumbKinds   = set("navigation", "tap", "network", "lifecycle", "custom")
)

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

// ParseReport decodes a report part. Unknown fields are ignored. A type
// mismatch (e.g. a number where a string is expected) is an error.
func ParseReport(data []byte) (*Report, error) {
	var r Report
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("not a valid report: %w", err)
	}
	if dec.More() {
		return nil, errors.New("not a valid report: unexpected data after the JSON object")
	}
	return &r, nil
}

// OccurredTime returns occurred_at as a time. Call it after Validate.
func (r *Report) OccurredTime() time.Time {
	t, _ := time.Parse(time.RFC3339Nano, r.OccurredAt)
	return t
}

// Normalize applies the rules that change the report rather than reject
// it: UUIDs are lowercased, so the same ID in another case is the same
// report, and request and response bodies are dropped unless
// bodies_captured is true. Call it after Validate.
func (r *Report) Normalize() {
	r.ID = strings.ToLower(r.ID)
	r.InstallationID = strings.ToLower(r.InstallationID)
	if r.BodiesCaptured {
		return
	}
	for i := range r.NetworkRequests {
		r.NetworkRequests[i].RequestBody = ""
		r.NetworkRequests[i].ResponseBody = ""
	}
}

// Validate checks the report against the schema. It returns
// ErrUnsupportedVersion for an unknown schema_version, and otherwise an
// error naming the first field that is wrong.
func (r *Report) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return ErrUnsupportedVersion
	}

	v := &validator{}
	v.uuid("id", r.ID)
	v.uuid("installation_id", r.InstallationID)
	v.text("session_id", r.SessionID, 1, 128)
	v.timestamp("occurred_at", r.OccurredAt)
	v.enum("platform", r.Platform, platforms, true)

	if r.SDK == nil {
		v.fail("sdk", "is required")
	} else {
		v.text("sdk.name", r.SDK.Name, 1, 64)
		v.text("sdk.version", r.SDK.Version, 1, 64)
	}

	v.text("environment", r.Environment, 1, 64)

	if r.Release == nil {
		v.fail("release", "is required")
	} else {
		v.text("release.version", r.Release.Version, 1, 64)
		v.text("release.build", r.Release.Build, 1, 64)
	}

	v.enum("mode", r.Mode, modes, false)
	v.enum("trigger", r.Trigger, triggers, true)
	v.enum("consent", r.Consent, consents, false)

	if u := r.UserInput; u != nil {
		v.text("user_input.title", u.Title, 0, 200)
		v.text("user_input.description", u.Description, 0, 10000)
		v.enum("user_input.category", u.Category, categories, false)
		v.enum("user_input.severity", u.Severity, severities, false)
	}

	if d := r.Device; d == nil {
		v.fail("device", "is required")
	} else {
		v.text("device.os_name", d.OSName, 1, 32)
		v.text("device.os_version", d.OSVersion, 1, 32)
		v.text("device.model", d.Model, 1, 64)
		v.text("device.locale", d.Locale, 0, 35)
		if b := d.BatteryLevel; b != nil && (*b < 0 || *b > 1) {
			v.fail("device.battery_level", "must be between 0 and 1")
		}
		if m := d.AvailableMemoryBytes; m != nil && *m < 0 {
			v.fail("device.available_memory_bytes", "must not be negative")
		}
		v.enum("device.network_type", d.NetworkType, networkTypes, false)
		v.enum("device.orientation", d.Orientation, orientations, false)
	}

	if len(r.FeatureFlags) > 200 {
		v.fail("feature_flags", "has more than 200 entries")
	}
	for name, value := range r.FeatureFlags {
		switch value.(type) {
		case string, float64, bool:
		default:
			v.fail("feature_flags."+name, "must be a string, a number or a boolean")
		}
	}
	if len(r.Experiments) > 100 {
		v.fail("experiments", "has more than 100 entries")
	}
	for name, variant := range r.Experiments {
		v.text("experiments."+name, variant, 0, 128)
	}

	v.max("logs", len(r.Logs), 500)
	for i, l := range r.Logs {
		p := fmt.Sprintf("logs[%d]", i)
		v.timestamp(p+".timestamp", l.Timestamp)
		v.enum(p+".level", l.Level, logLevels, false)
		v.text(p+".message", l.Message, 0, 4000)
	}

	v.max("breadcrumbs", len(r.Breadcrumbs), 200)
	for i, b := range r.Breadcrumbs {
		p := fmt.Sprintf("breadcrumbs[%d]", i)
		v.timestamp(p+".timestamp", b.Timestamp)
		v.enum(p+".kind", b.Kind, crumbKinds, true)
		v.text(p+".message", b.Message, 0, 1000)
	}

	if c := r.Crash; c != nil {
		v.text("crash.type", c.Type, 1, 256)
		v.text("crash.reason", c.Reason, 0, 4000)
		if len(c.StackTrace) == 0 {
			v.fail("crash.stack_trace", "must have at least one frame")
		}
		v.max("crash.stack_trace", len(c.StackTrace), 512)
		for i, frame := range c.StackTrace {
			v.text(fmt.Sprintf("crash.stack_trace[%d]", i), frame, 0, 1000)
		}
	} else if r.Trigger == "crash" || r.Trigger == "non_fatal" {
		v.fail("crash", "is required when trigger is "+r.Trigger)
	}

	v.max("network_requests", len(r.NetworkRequests), 100)
	for i, n := range r.NetworkRequests {
		p := fmt.Sprintf("network_requests[%d]", i)
		v.timestamp(p+".timestamp", n.Timestamp)
		v.text(p+".url", n.URL, 0, 2048)
		v.text(p+".method", n.Method, 1, 16)
		if s := n.StatusCode; s != nil && (*s < 100 || *s > 599) {
			v.fail(p+".status_code", "must be between 100 and 599")
		}
		if n.DurationMS == nil {
			v.fail(p+".duration_ms", "is required")
		} else if *n.DurationMS < 0 {
			v.fail(p+".duration_ms", "must not be negative")
		}
		v.text(p+".request_body", n.RequestBody, 0, 16000)
		v.text(p+".response_body", n.ResponseBody, 0, 16000)
	}

	if c := r.Crashlytics; c != nil {
		v.text("crashlytics.installation_id", c.InstallationID, 0, 128)
	}

	return v.err
}

// validator keeps the first failure, so the SDK gets one clear message.
type validator struct {
	err error
}

func (v *validator) fail(field, msg string) {
	if v.err == nil {
		v.err = fmt.Errorf("%s: %s", field, msg)
	}
}

func (v *validator) uuid(field, value string) {
	if !uuidPattern.MatchString(value) {
		v.fail(field, "must be a UUID")
	}
}

func (v *validator) timestamp(field, value string) {
	if !timestampPattern.MatchString(value) {
		v.fail(field, "must be an RFC 3339 UTC timestamp with milliseconds, e.g. 2026-10-18T14:03:12.123Z")
		return
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		v.fail(field, "is not a valid date and time")
	}
}

// text checks a string's length in characters. A minimum of 1 makes the
// field required.
func (v *validator) text(field, value string, min, max int) {
	n := utf8.RuneCountInString(value)
	switch {
	case n < min && n == 0:
		v.fail(field, "is required")
	case n < min:
		v.fail(field, fmt.Sprintf("must have at least %d characters", min))
	case n > max:
		v.fail(field, fmt.Sprintf("must have at most %d characters", max))
	}
}

func (v *validator) enum(field, value string, allowed map[string]bool, required bool) {
	if value == "" {
		if required {
			v.fail(field, "is required")
		}
		return
	}
	if !allowed[value] {
		v.fail(field, fmt.Sprintf("unknown value %q", value))
	}
}

func (v *validator) max(field string, n, max int) {
	if n > max {
		v.fail(field, fmt.Sprintf("has more than %d items", max))
	}
}
