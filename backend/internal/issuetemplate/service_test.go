package issuetemplate

import (
	"testing"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
)

func TestValidateName(t *testing.T) {
	if _, err := validateName("  "); kind(err) != apperr.KindInvalid {
		t.Errorf("blank name: err = %v", err)
	}
	if _, err := validateName(" Crash template "); err != nil {
		t.Fatalf("err = %v", err)
	} else {
		got, _ := validateName(" Crash template ")
		if got != "Crash template" {
			t.Errorf("got %q", got)
		}
	}
}

func TestValidateTemplate(t *testing.T) {
	if _, err := validateTemplate("title", ""); kind(err) != apperr.KindInvalid {
		t.Errorf("empty template: err = %v", err)
	}
	if _, err := validateTemplate("title", "{{if .Title}}no matching end"); kind(err) != apperr.KindInvalid {
		t.Errorf("unparseable template: err = %v", err)
	}
	if _, err := validateTemplate("title", "{{.NotAField}}"); kind(err) != apperr.KindInvalid {
		t.Errorf("unknown field: err = %v", err)
	}
	if _, err := validateTemplate("title", "Bug: {{.Title}}"); err != nil {
		t.Errorf("valid template: err = %v", err)
	}
}

func kind(err error) apperr.Kind {
	k, _ := apperr.Classify(err)
	return k
}
