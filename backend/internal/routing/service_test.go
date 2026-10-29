package routing

import (
	"slices"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	got, err := names("labels", []string{" bug ", "", "bug", "ios"}, 3, 10)
	if err != nil || !slices.Equal(got, []string{"bug", "ios"}) {
		t.Errorf("got %v, %v; want trimmed, without empty and duplicate entries", got, err)
	}
	if _, err := names("labels", []string{"a", "b", "c", "d"}, 3, 10); err == nil {
		t.Error("accepted too many entries")
	}
	if _, err := names("labels", []string{strings.Repeat("é", 11)}, 3, 10); err == nil {
		t.Error("accepted an entry that is too long")
	}
}

func TestOneOf(t *testing.T) {
	for _, v := range []*string{nil, new(string)} {
		if got, err := oneOf("severity", v, severities); got != nil || err != nil {
			t.Errorf("empty value: got %v, %v; want no condition", got, err)
		}
	}
	high, loud := "high", "loud"
	if got, err := oneOf("severity", &high, severities); err != nil || *got != "high" {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := oneOf("severity", &loud, severities); err == nil {
		t.Error("accepted an unknown severity")
	}
}
