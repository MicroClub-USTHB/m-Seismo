package triage

import (
	"slices"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

func TestParseCodeowners(t *testing.T) {
	t.Run("comment and whitespace handling", func(t *testing.T) {
		raw := `
# Global default
*                   @core-team

   # Indented comment line

/docs/              @docs-team	# with trailing tab and comment
*.swift             @ios-devs @lead-ios
backend/            lead@backend.org
invalid-without-owners
`
		co := ParseCodeowners([]byte(raw))
		rules := co.Rules()
		if len(rules) != 4 {
			t.Fatalf("expected 4 rules, got %d", len(rules))
		}

		if rules[0].Pattern != "*" || !slices.Equal(rules[0].Owners, []string{"@core-team"}) {
			t.Errorf("unexpected rule 0: %+v", rules[0])
		}
		if rules[1].Pattern != "/docs/" || !slices.Equal(rules[1].Owners, []string{"@docs-team"}) {
			t.Errorf("unexpected rule 1: %+v", rules[1])
		}
		if rules[2].Pattern != "*.swift" || !slices.Equal(rules[2].Owners, []string{"@ios-devs", "@lead-ios"}) {
			t.Errorf("unexpected rule 2: %+v", rules[2])
		}
		if rules[3].Pattern != "backend/" || !slices.Equal(rules[3].Owners, []string{"lead@backend.org"}) {
			t.Errorf("unexpected rule 3: %+v", rules[3])
		}
	})

	t.Run("empty or nil content", func(t *testing.T) {
		coNil := ParseCodeowners(nil)
		if len(coNil.Rules()) != 0 {
			t.Errorf("expected 0 rules for nil, got %d", len(coNil.Rules()))
		}

		owners, pattern := coNil.Match("foo.go")
		if owners != nil || pattern != "" {
			t.Errorf("expected no match on empty codeowners")
		}
	})
}

func TestCodeowners_Match(t *testing.T) {
	t.Run("precedence last match wins", func(t *testing.T) {
		raw := `
* @global
*.swift @ios-lead
Features/Auth/*.swift @auth-team
`
		co := ParseCodeowners([]byte(raw))

		// 1. Matches Features/Auth/*.swift -> @auth-team
		owners, pattern := co.Match("Features/Auth/LoginView.swift")
		if pattern != "Features/Auth/*.swift" || len(owners) == 0 || owners[0] != "@auth-team" {
			t.Errorf("expected @auth-team for LoginView.swift, got pattern %q, owners %v", pattern, owners)
		}

		// 2. Matches *.swift -> @ios-lead
		owners, pattern = co.Match("Features/Cart/CartView.swift")
		if pattern != "*.swift" || len(owners) == 0 || owners[0] != "@ios-lead" {
			t.Errorf("expected @ios-lead for CartView.swift, got pattern %q, owners %v", pattern, owners)
		}

		// 3. Fallback to * -> @global
		owners, pattern = co.Match("cmd/server/main.go")
		if pattern != "*" || len(owners) == 0 || owners[0] != "@global" {
			t.Errorf("expected @global for main.go, got pattern %q, owners %v", pattern, owners)
		}
	})

	t.Run("directory matching", func(t *testing.T) {
		raw := `
/backend/ @backend-team
Features/Checkout/ @checkout-team
`
		co := ParseCodeowners([]byte(raw))

		owners, pattern := co.Match("backend/internal/worker/process.go")
		if pattern != "/backend/" || len(owners) == 0 || owners[0] != "@backend-team" {
			t.Errorf("expected @backend-team for process.go, got pattern %q, owners %v", pattern, owners)
		}

		owners, pattern = co.Match("Features/Checkout/CartViewController.swift")
		if pattern != "Features/Checkout/" || len(owners) == 0 || owners[0] != "@checkout-team" {
			t.Errorf("expected @checkout-team for CartViewController.swift, got pattern %q, owners %v", pattern, owners)
		}
	})
}

func TestExtractPathsFromCrash(t *testing.T) {
	crash := &ingest.Crash{
		Type: "FatalError",
		StackTrace: []string{
			"0   AppCore    0x000100001234 Features/Checkout/CheckoutViewController.swift:42",
			"\t/home/moncef/backend/internal/worker/process.go:110 +0x123",
			"at com.example.app.AuthActivity.onCreate(AuthActivity.java:55)",
			`File "/app/services/payment.py", line 88, in charge`,
		},
	}

	paths := ExtractPathsFromCrash(crash)
	expected := []string{
		"Features/Checkout/CheckoutViewController.swift",
		"home/moncef/backend/internal/worker/process.go",
		"AuthActivity.java",
		"app/services/payment.py",
	}

	if len(paths) != len(expected) {
		t.Fatalf("expected %d extracted paths, got %d: %v", len(expected), len(paths), paths)
	}

	for i, exp := range expected {
		if paths[i] != exp {
			t.Errorf("path[%d] expected %q, got %q", i, exp, paths[i])
		}
	}
}

func TestMatchOwner(t *testing.T) {
	raw := `
* @global
/backend/ @backend-team
Features/Checkout/ @checkout-team
AuthActivity.java @auth-team
ProfileSettings @settings-team
`
	co := ParseCodeowners([]byte(raw))

	t.Run("crash stack frame extraction matches owner", func(t *testing.T) {
		// Swift stack frame match
		rSwift := &ingest.Report{
			Crash: &ingest.Crash{
				Type: "Crash",
				StackTrace: []string{
					"0   AppCore 0x0001 Features/Checkout/CheckoutViewController.swift:42",
				},
			},
		}
		owner, reason := MatchOwner(co, rSwift, "HomeScreen")
		if owner != "@checkout-team" {
			t.Errorf("expected @checkout-team, got %q", owner)
		}
		if !strings.Contains(reason, "Features/Checkout/") || !strings.Contains(reason, "CheckoutViewController.swift") {
			t.Errorf("expected reason to cite rule and frame, got %q", reason)
		}

		// Java stack frame match
		rJava := &ingest.Report{
			Crash: &ingest.Crash{
				Type: "NullPointerException",
				StackTrace: []string{
					"at com.example.app.AuthActivity.onCreate(AuthActivity.java:55)",
				},
			},
		}
		owner, reason = MatchOwner(co, rJava, "")
		if owner != "@auth-team" {
			t.Errorf("expected @auth-team, got %q", owner)
		}
		if !strings.Contains(reason, "AuthActivity.java") {
			t.Errorf("expected reason to cite rule, got %q", reason)
		}

		// Go stack frame match
		rGo := &ingest.Report{
			Crash: &ingest.Crash{
				Type: "panic",
				StackTrace: []string{
					"\t/home/moncef/backend/internal/worker/process.go:110 +0x123",
				},
			},
		}
		owner, reason = MatchOwner(co, rGo, "")
		if owner != "@backend-team" {
			t.Errorf("expected @backend-team, got %q", owner)
		}
		if !strings.Contains(reason, "/backend/") {
			t.Errorf("expected reason to cite rule, got %q", reason)
		}
	})

	t.Run("fallback to screen when no stack trace", func(t *testing.T) {
		r := &ingest.Report{
			Breadcrumbs: []ingest.Breadcrumb{
				{Kind: "navigation", Message: "ProfileSettings"},
			},
		}
		owner, reason := MatchOwner(co, r, "ProfileSettings")
		if owner != "@settings-team" {
			t.Errorf("expected @settings-team, got %q", owner)
		}
		if !strings.Contains(reason, "ProfileSettings") {
			t.Errorf("expected reason to cite screen, got %q", reason)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		emptyCo := ParseCodeowners([]byte(""))
		r := &ingest.Report{
			Breadcrumbs: []ingest.Breadcrumb{
				{Kind: "navigation", Message: "Unknown"},
			},
		}
		owner, reason := MatchOwner(emptyCo, r, "Unknown")
		if owner != "" || reason != "" {
			t.Errorf("expected empty owner and reason, got owner=%q reason=%q", owner, reason)
		}
	})
}

func TestAnalyzeWithOptions(t *testing.T) {
	raw := `
Features/Checkout/ @checkout-team
ProfileSettings @settings-team
`
	co := ParseCodeowners([]byte(raw))

	r := &ingest.Report{
		Platform: "ios",
		Trigger:  "crash",
		Crash: &ingest.Crash{
			Type: "EXC_BAD_ACCESS",
			StackTrace: []string{
				"0 AppCore Features/Checkout/CartView.swift:20",
			},
		},
		Breadcrumbs: []ingest.Breadcrumb{
			{Kind: "navigation", Message: "ProfileSettings"},
		},
	}

	s := AnalyzeWithOptions(r, Options{Codeowners: co})
	if s.Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical, got %q", s.Severity)
	}
	if s.Owner != "@checkout-team" {
		t.Errorf("expected owner @checkout-team, got %q", s.Owner)
	}
	if !strings.Contains(s.OwnerReason, "Features/Checkout/") {
		t.Errorf("expected OwnerReason to mention matched rule, got %q", s.OwnerReason)
	}
}
