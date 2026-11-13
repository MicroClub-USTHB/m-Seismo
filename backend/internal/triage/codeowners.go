package triage

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

var (
	// Matches: ... (filename.ext:123) or ... (path/to/file.ext:123:45)
	parenLineRegex = regexp.MustCompile(`\(([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9_]+):(\d+)(?::\d+)?\)`)
	// Matches: path/to/file.ext:123
	colonLineRegex = regexp.MustCompile(`([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9_]+):(\d+)`)
	// Matches: File "path/to/file.ext", line 123
	pythonLineRegex = regexp.MustCompile(`File "([^"]+)", line \d+`)
)

// Rule represents a single entry in a CODEOWNERS file.
type Rule struct {
	Pattern string
	Owners  []string
}

// Codeowners holds parsed rules from a repository's CODEOWNERS file.
type Codeowners struct {
	rules []Rule
}

// Rules returns a copy of the parsed rules.
func (c *Codeowners) Rules() []Rule {
	if c == nil {
		return nil
	}
	return slices.Clone(c.rules)
}

// ParseCodeowners parses the content of a CODEOWNERS file.
// Lines starting with # or empty lines are ignored. Order is preserved.
func ParseCodeowners(content []byte) *Codeowners {
	co := &Codeowners{}
	if len(content) == 0 {
		return co
	}

	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf")) // trim UTF-8 BOM if present
	scanner := bufio.NewScanner(bytes.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		pattern := fields[0]
		var owners []string
		for _, token := range fields[1:] {
			if strings.HasPrefix(token, "#") {
				break
			}
			if strings.HasPrefix(token, "@") || strings.Contains(token, "@") {
				owners = append(owners, token)
			}
		}

		if len(owners) > 0 {
			co.rules = append(co.rules, Rule{
				Pattern: pattern,
				Owners:  owners,
			})
		}
	}

	return co
}

// Match evaluates a path against the CODEOWNERS rules in reverse order
// (last matching pattern wins). It returns the owners and the matched pattern.
func (c *Codeowners) Match(pathStr string) (owners []string, matchedPattern string) {
	if c == nil || len(c.rules) == 0 || pathStr == "" {
		return nil, ""
	}

	normPath := filepath.ToSlash(strings.TrimSpace(pathStr))
	normPath = strings.TrimPrefix(normPath, "/")

	for i := len(c.rules) - 1; i >= 0; i-- {
		rule := c.rules[i]
		if pathMatches(rule.Pattern, normPath) {
			return rule.Owners, rule.Pattern
		}
	}

	return nil, ""
}

func pathMatches(pattern, normPath string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || normPath == "" {
		return false
	}
	if pattern == "*" {
		return true
	}

	// Pattern like *.swift or *.go
	if strings.HasPrefix(pattern, "*.") {
		ext := pattern[1:]
		if strings.HasSuffix(normPath, ext) {
			return true
		}
	}

	// Directory match (e.g. /backend/ or Features/Checkout/)
	if strings.HasSuffix(pattern, "/") {
		trimmed := strings.Trim(pattern, "/")
		if trimmed == "" {
			return true
		}
		if strings.HasPrefix(normPath, trimmed+"/") ||
			strings.Contains(normPath, "/"+trimmed+"/") ||
			strings.Contains(normPath, trimmed) {
			return true
		}
	}

	// Strip leading slash for relative matching
	cleanPattern := strings.TrimPrefix(pattern, "/")

	// Direct match or standard glob match against the full path
	if cleanPattern == normPath {
		return true
	}
	if matched, err := path.Match(cleanPattern, normPath); err == nil && matched {
		return true
	}

	// Match basename if pattern has no slash (e.g. "CartViewController.swift")
	if !strings.Contains(cleanPattern, "/") {
		base := path.Base(normPath)
		if cleanPattern == base {
			return true
		}
		if matched, err := path.Match(cleanPattern, base); err == nil && matched {
			return true
		}
	}

	// Subdirectory match for cleanPattern
	if strings.HasPrefix(normPath, cleanPattern+"/") || strings.Contains(normPath, "/"+cleanPattern+"/") {
		return true
	}

	return false
}

// ExtractPathsFromCrash inspects crash stack traces and extracts candidate filenames
// in order of stack priority (top frames first).
func ExtractPathsFromCrash(crash *ingest.Crash) []string {
	if crash == nil || len(crash.StackTrace) == 0 {
		return nil
	}

	var candidates []string
	add := func(p string) {
		p = filepath.ToSlash(strings.TrimSpace(p))
		p = strings.TrimPrefix(p, "/")
		if p == "" {
			return
		}
		if !slices.Contains(candidates, p) {
			candidates = append(candidates, p)
		}
	}

	for _, line := range crash.StackTrace {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if m := parenLineRegex.FindStringSubmatch(line); len(m) > 1 {
			add(m[1])
			continue
		}
		if m := pythonLineRegex.FindStringSubmatch(line); len(m) > 1 {
			add(m[1])
			continue
		}
		if m := colonLineRegex.FindStringSubmatch(line); len(m) > 1 {
			add(m[1])
			continue
		}
	}

	return candidates
}

// ExtractScreenPath sanitizes and formats a screen identifier into a candidate path.
func ExtractScreenPath(screen string) string {
	s := strings.TrimSpace(screen)
	s = strings.TrimPrefix(s, "screen:")
	s = strings.TrimPrefix(s, "Screen:")
	return strings.TrimSpace(s)
}

// MatchOwner resolves the best owner for a report using parsed CODEOWNERS rules.
// It first attempts to match stack trace frames, then falls back to the navigation screen.
func MatchOwner(c *Codeowners, r *ingest.Report, screen string) (owner string, reason string) {
	if c == nil || r == nil {
		return "", ""
	}

	if r.Crash != nil {
		paths := ExtractPathsFromCrash(r.Crash)
		for _, framePath := range paths {
			owners, pattern := c.Match(framePath)
			if len(owners) > 0 {
				return owners[0], fmt.Sprintf("matched CODEOWNERS rule %q with stack frame %q", pattern, framePath)
			}
		}
	}

	screenPath := ExtractScreenPath(screen)
	if screenPath != "" {
		owners, pattern := c.Match(screenPath)
		if len(owners) > 0 {
			return owners[0], fmt.Sprintf("matched CODEOWNERS rule %q with screen %q", pattern, screenPath)
		}

		trimmed := strings.TrimSuffix(strings.TrimSuffix(screenPath, "Screen"), "View")
		if trimmed != "" && trimmed != screenPath {
			owners, pattern := c.Match(trimmed)
			if len(owners) > 0 {
				return owners[0], fmt.Sprintf("matched CODEOWNERS rule %q with screen %q", pattern, screenPath)
			}
		}
	}

	return "", ""
}
