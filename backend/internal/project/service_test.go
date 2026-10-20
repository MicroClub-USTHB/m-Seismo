package project

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"My App":                "my-app",
		"  iOS -- Beta!  ":      "ios-beta",
		"Ünïcode Ápp 2":         "n-code-pp-2",
		strings.Repeat("a", 80): strings.Repeat("a", 64),
	}
	for in, want := range tests {
		got := slugify(in)
		if got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
		if err := validateSlug(got); err != nil {
			t.Errorf("slugify(%q) produced an invalid slug: %v", in, err)
		}
	}
}

func TestValidateSlug(t *testing.T) {
	for _, bad := range []string{"", "Upper", "a--b", "-a", "a-", "a b", strings.Repeat("a", 65)} {
		if validateSlug(bad) == nil {
			t.Errorf("validateSlug(%q) accepted", bad)
		}
	}
}
