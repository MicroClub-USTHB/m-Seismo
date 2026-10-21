// Command fakegithub is a development-only stand-in for github.com and
// api.github.com. It implements just the endpoints m-Seismo's GitHub
// provider calls, so the connect flow and issue opening can be exercised
// end to end without a real GitHub App or a browser.
//
// On start it writes a fresh RSA private key to FAKEGITHUB_KEY_PATH for the
// backend to sign App JWTs with. Signatures are not verified; the fake only
// checks that the right kind of token is sent to each endpoint.
//
// Fixtures:
//
//	client secret      fake-secret
//	OAuth code         fake-code-<N>  → user token fake-user-token-<N>,
//	                                    who can access installation N only
//	installation N     on "octo-org" (Organization), token fake-installation-token-<N>
//	repositories       octo-org/seismo-test, octo-org/another,
//	                   octo-org/broken     (issue creation always fails with 500)
//	                   octo-org/slow-down  (first create per title: secondary rate
//	                                        limit, 403 + Retry-After: 1)
//	                   octo-org/exhausted  (create: primary quota exhausted,
//	                                        403 + X-RateLimit-Remaining: 0, reset in 1h)
//	milestone          "v1.0" (number 1) in every repository
//	rate-limit headers X-RateLimit-Limit/Remaining/Reset/Resource on every
//	                   installation response (5000 per installation per hour)
//
// Any positive N works, so every test run can use a fresh installation and
// never collide with one that another owner already connected.
//
// Inspection endpoints for tests:
//
//	GET    /_fake/issues   every issue opened so far
//	DELETE /_fake/issues   forget them
//	GET    /_fake/stats    how many requests were rate-limited
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	clientSecret            = "fake-secret"
	oauthCodePrefix         = "fake-code-"
	userTokenPrefix         = "fake-user-token-"
	installationTokenPrefix = "fake-installation-token-"
	accountLogin            = "octo-org"
	brokenRepository        = "octo-org/broken"
	slowDownRepository      = "octo-org/slow-down"
	exhaustedRepository     = "octo-org/exhausted"
	hourlyQuota             = 5000
)

var repositories = []string{
	"octo-org/seismo-test", "octo-org/another",
	brokenRepository, slowDownRepository, exhaustedRepository,
}

type storedIssue struct {
	Repository string   `json:"repository"`
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Labels     []string `json:"labels"`
	Assignees  []string `json:"assignees"`
	Milestone  *int     `json:"milestone"`
	State      string   `json:"state"`
	Comments   []string `json:"comments"`
	HTMLURL    string   `json:"html_url"`
	CreatedAt  string   `json:"created_at"`
}

type server struct {
	publicURL string
	mu        sync.Mutex
	issues    []*storedIssue

	// used counts requests per installation token for the rate-limit headers.
	used map[string]int
	// slowDownSeen remembers titles already rate-limited on slow-down.
	slowDownSeen     map[string]bool
	secondaryLimited int
	primaryLimited   int
}

func main() {
	addr := env("FAKEGITHUB_ADDR", ":9090")
	keyPath := env("FAKEGITHUB_KEY_PATH", "/shared/github-app.pem")

	if err := writeKey(keyPath); err != nil {
		log.Fatalf("fakegithub: write key: %v", err)
	}
	log.Printf("fakegithub: wrote app private key to %s", keyPath)

	s := &server{
		publicURL:    strings.TrimRight(env("FAKEGITHUB_PUBLIC_URL", "http://localhost:9090"), "/"),
		used:         map[string]int{},
		slowDownSeen: map[string]bool{},
	}
	log.Printf("fakegithub: listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, s.routes()))
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// web (github.com)
	mux.HandleFunc("GET /apps/{slug}/installations/new", s.installPage)
	mux.HandleFunc("POST /login/oauth/access_token", s.exchangeCode)

	// API as the user
	mux.HandleFunc("GET /user/installations", s.requireToken(userTokenPrefix, s.userInstallations))

	// API as the App (JWT)
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", s.mintInstallationToken)

	// API as an installation
	mux.HandleFunc("GET /installation/repositories", s.requireToken(installationTokenPrefix, s.installationRepos))
	mux.HandleFunc("GET /repos/{owner}/{repo}", s.requireToken(installationTokenPrefix, s.getRepo))
	mux.HandleFunc("GET /repos/{owner}/{repo}/milestones", s.requireToken(installationTokenPrefix, s.milestones))
	mux.HandleFunc("POST /repos/{owner}/{repo}/issues", s.requireToken(installationTokenPrefix, s.createIssue))
	mux.HandleFunc("GET /repos/{owner}/{repo}/issues/{number}", s.requireToken(installationTokenPrefix, s.getIssue))
	mux.HandleFunc("POST /repos/{owner}/{repo}/issues/{number}/comments", s.requireToken(installationTokenPrefix, s.createComment))

	// test inspection
	mux.HandleFunc("GET /_fake/issues", s.listIssues)
	mux.HandleFunc("DELETE /_fake/issues", s.resetIssues)
	mux.HandleFunc("GET /_fake/stats", s.stats)

	return logRequests(mux)
}

// installPage stands in for the GitHub App install screen and explains how
// to finish the flow by hand, since there is no real UI.
func (s *server) installPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "fakegithub install page for app %q.\n\n", r.PathValue("slug"))
	fmt.Fprintf(w, "To finish, pick any number N and call the m-Seismo callback with:\n")
	fmt.Fprintf(w, "  installation_id=N&setup_action=install&code=%sN&state=%s\n", oauthCodePrefix, r.URL.Query().Get("state"))
}

func (s *server) exchangeCode(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}
	if r.Form.Get("client_secret") != clientSecret {
		writeJSON(w, http.StatusOK, map[string]string{"error": "incorrect_client_credentials"})
		return
	}
	id, ok := numericSuffix(r.Form.Get("code"), oauthCodePrefix)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]string{"error": "bad_verification_code"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"access_token": userTokenPrefix + id, "token_type": "bearer", "scope": ""})
}

// userInstallations lists the one installation the user token was minted for.
func (s *server) userInstallations(w http.ResponseWriter, r *http.Request) {
	id, _ := numericSuffix(bearer(r), userTokenPrefix)
	n, _ := strconv.Atoi(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"total_count":   1,
		"installations": []any{installation(n)},
	})
}

func (s *server) mintInstallationToken(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	id, ok := numericSuffix(r.PathValue("id"), "")
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      installationTokenPrefix + id,
		"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
}

func (s *server) installationRepos(w http.ResponseWriter, _ *http.Request) {
	repos := make([]any, len(repositories))
	for i, full := range repositories {
		repos[i] = s.repo(full)
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(repos), "repositories": repos})
}

func (s *server) getRepo(w http.ResponseWriter, r *http.Request) {
	full, ok := knownRepo(r)
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, s.repo(full))
}

func (s *server) milestones(w http.ResponseWriter, r *http.Request) {
	if _, ok := knownRepo(r); !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, []any{map[string]any{"number": 1, "title": "v1.0", "state": "open"}})
}

func (s *server) createIssue(w http.ResponseWriter, r *http.Request) {
	full, ok := knownRepo(r)
	if !ok {
		notFound(w)
		return
	}
	var req struct {
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Labels    []string `json:"labels"`
		Assignees []string `json:"assignees"`
		Milestone *int     `json:"milestone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Validation Failed"})
		return
	}
	switch full {
	case brokenRepository:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Server Error (simulated by fakegithub)"})
		return
	case exhaustedRepository:
		s.mu.Lock()
		s.primaryLimited++
		s.mu.Unlock()
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		writeJSON(w, http.StatusForbidden, map[string]string{
			"message":           "API rate limit exceeded for installation.",
			"documentation_url": "https://docs.github.com/rest/overview/rate-limits-for-the-rest-api",
		})
		return
	case slowDownRepository:
		s.mu.Lock()
		first := !s.slowDownSeen[req.Title]
		s.slowDownSeen[req.Title] = true
		if first {
			s.secondaryLimited++
		}
		s.mu.Unlock()
		if first {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusForbidden, map[string]string{
				"message":           "You have exceeded a secondary rate limit. Please wait a few minutes before you try again.",
				"documentation_url": "https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits",
			})
			return
		}
	}

	s.mu.Lock()
	number := 1
	for _, is := range s.issues {
		if is.Repository == full {
			number++
		}
	}
	is := &storedIssue{
		Repository: full,
		Number:     number,
		Title:      req.Title,
		Body:       req.Body,
		Labels:     orEmpty(req.Labels),
		Assignees:  orEmpty(req.Assignees),
		Milestone:  req.Milestone,
		State:      "open",
		Comments:   []string{},
		HTMLURL:    fmt.Sprintf("%s/%s/issues/%d", s.publicURL, full, number),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	s.issues = append(s.issues, is)
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, issueJSON(is))
}

func (s *server) getIssue(w http.ResponseWriter, r *http.Request) {
	is := s.findIssue(r)
	if is == nil {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, issueJSON(is))
}

func (s *server) createComment(w http.ResponseWriter, r *http.Request) {
	is := s.findIssue(r)
	if is == nil {
		notFound(w)
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	is.Comments = append(is.Comments, req.Body)
	n := len(is.Comments)
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"id": n, "body": req.Body})
}

func (s *server) listIssues(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*storedIssue, len(s.issues))
	copy(out, s.issues)
	writeJSON(w, http.StatusOK, out)
}

func (s *server) resetIssues(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.issues = nil
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) stats(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]int{
		"secondary_limited": s.secondaryLimited,
		"primary_limited":   s.primaryLimited,
		"issues":            len(s.issues),
	})
}

func (s *server) findIssue(r *http.Request) *storedIssue {
	full, ok := knownRepo(r)
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, is := range s.issues {
		if is.Repository == full && is.Number == n {
			return is
		}
	}
	return nil
}

func (s *server) repo(full string) map[string]any {
	return map[string]any{
		"full_name": full,
		"name":      full[strings.Index(full, "/")+1:],
		"private":   true,
		"html_url":  s.publicURL + "/" + full,
	}
}

// requireToken accepts a bearer token of the form <prefix><N> and adds
// GitHub's rate-limit headers, counting requests per token.
func (s *server) requireToken(prefix string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if _, ok := numericSuffix(token, prefix); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return
		}
		s.mu.Lock()
		s.used[token]++
		remaining := max(hourlyQuota-s.used[token], 0)
		s.mu.Unlock()
		h := w.Header()
		h.Set("X-RateLimit-Limit", strconv.Itoa(hourlyQuota))
		h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		h.Set("X-RateLimit-Used", strconv.Itoa(hourlyQuota-remaining))
		h.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Truncate(time.Hour).Add(time.Hour).Unix(), 10))
		h.Set("X-RateLimit-Resource", "core")
		next(w, r)
	}
}

// bearer returns the token from an "Authorization: Bearer x" or "token x" header.
func bearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return t
	}
	t, _ := strings.CutPrefix(auth, "token ")
	return t
}

// numericSuffix returns what follows prefix in s when it is a positive integer.
func numericSuffix(s, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return "", false
	}
	if n, err := strconv.Atoi(rest); err != nil || n <= 0 {
		return "", false
	}
	return rest, true
}

func installation(id int) map[string]any {
	return map[string]any{
		"id":      id,
		"account": map[string]any{"login": accountLogin, "type": "Organization"},
	}
}

func issueJSON(is *storedIssue) map[string]any {
	return map[string]any{
		"number":   is.Number,
		"title":    is.Title,
		"body":     is.Body,
		"state":    is.State,
		"html_url": is.HTMLURL,
	}
}

func knownRepo(r *http.Request) (string, bool) {
	full := r.PathValue("owner") + "/" + r.PathValue("repo")
	return full, slices.Contains(repositories, full)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeKey(path string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o644)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
