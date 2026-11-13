package issuetemplate

import (
	"fmt"
	"io"
	"strings"
	"text/template"
)

// TemplateData is what a project's issue template (FR-50) can use: the
// report's title, steps, crash, environment and logs, each already
// rendered as markdown by the worker.
type TemplateData struct {
	Title       string
	Steps       string
	Crash       string
	Environment string
	Logs        string
	Triage      string
}

// compile parses tpl as a Go text/template. Referencing a field TemplateData
// doesn't have fails only once it is executed, not here; Render and
// validateTemplate both execute against a value to catch that too.
func compile(name, tpl string) (*template.Template, error) {
	return template.New(name).Parse(tpl)
}

// Render executes titleTpl and bodyTpl against data.
func Render(titleTpl, bodyTpl string, data TemplateData) (title, body string, err error) {
	t, err := compile("title", titleTpl)
	if err != nil {
		return "", "", fmt.Errorf("title_template: %w", err)
	}
	var tb strings.Builder
	if err := t.Execute(&tb, data); err != nil {
		return "", "", fmt.Errorf("title_template: %w", err)
	}

	b, err := compile("body", bodyTpl)
	if err != nil {
		return "", "", fmt.Errorf("body_template: %w", err)
	}
	var bb strings.Builder
	if err := b.Execute(&bb, data); err != nil {
		return "", "", fmt.Errorf("body_template: %w", err)
	}
	return tb.String(), bb.String(), nil
}

// check parses tpl and executes it against a zero TemplateData, so a typo'd
// field (e.g. {{.Ttile}}) is refused when the template is saved rather than
// the next time an issue is opened with it.
func check(tpl string) error {
	t, err := compile("check", tpl)
	if err != nil {
		return err
	}
	return t.Execute(io.Discard, TemplateData{})
}
