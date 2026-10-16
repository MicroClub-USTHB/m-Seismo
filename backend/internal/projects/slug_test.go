package projects

import "testing"

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Club App":            "club-app",
		"  --Hello__World!! ": "hello-world",
		"Été 2026":            "t-2026",
		"":                    "",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
