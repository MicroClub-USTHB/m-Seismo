// Package fakebitbucket is a development and test stand-in for Bitbucket
// Cloud: the OAuth endpoints (bitbucket.org/site/oauth2/…) and the handful
// of REST API 2.0 endpoints (api.bitbucket.org/2.0/…) the bitbucket
// provider calls, served from one host. The provider's unit tests run it
// in-process and cmd/fakebitbucket serves it in the dev stack.
//
// Fixtures:
//
//	consumer           fake-client / fake-secret (HTTP Basic, like Bitbucket)
//	OAuth code         fake-code-<N>        → user N, access token valid 2 h
//	                   fake-code-<N>-short  → user N, access token valid 1 s
//	user N             uuid {0000000N-0000-4000-8000-000000000000},
//	                   username bb-user-<N>, account_id bb-account-<N>
//	tokens             access bb-access-<N>-<S> (S counts the access tokens
//	                   issued to user N), refresh bb-refresh-<N>-<K> (K
//	                   counts user N's grants). Every code exchange is a
//	                   separate grant. Like Bitbucket, a grant's refresh
//	                   token stays the same; a refresh replaces the grant's
//	                   access token.
//	repositories       seismo-ws/seismo-test, seismo-ws/another,
//	                   seismo-ws/no-issues (issue tracker disabled: 404),
//	                   seismo-ws/broken    (issue creation: 500)
//	                   seismo-ws/limited   (first create per title: 429, Retry-After 1)
//	                   seismo-ws/exhausted (issue creation: 429, Retry-After 3600)
//	milestone          "v1.0" (id 1) in every repository
//
// Inspection endpoints:
//
//	GET    /_fake/issues        every issue opened
//	DELETE /_fake/issues        forget them
//	GET    /_fake/stats         exchanges, refreshes, rate_limited, issues
//	POST   /_fake/revoke?user=N revoke user N's tokens
package fakebitbucket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ClientID     = "fake-client"
	ClientSecret = "fake-secret"
	CodePrefix   = "fake-code-"
	Workspace    = "seismo-ws"

	NoIssuesRepo  = Workspace + "/no-issues"
	BrokenRepo    = Workspace + "/broken"
	LimitedRepo   = Workspace + "/limited"
	ExhaustedRepo = Workspace + "/exhausted"
)

// Repositories every user can see, in listing order.
var Repositories = []string{
	Workspace + "/seismo-test", Workspace + "/another",
	NoIssuesRepo, BrokenRepo, LimitedRepo, ExhaustedRepo,
}

// UserUUID is the Bitbucket UUID of fake user N.
func UserUUID(n int) string { return fmt.Sprintf("{%08d-0000-4000-8000-000000000000}", n) }

// Issue is an issue opened on the fake.
type Issue struct {
	Repository string   `json:"repository"`
	ID         int      `json:"id"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Kind       string   `json:"kind"`
	Priority   string   `json:"priority"`
	Milestone  string   `json:"milestone"`
	State      string   `json:"state"`
	Comments   []string `json:"comments"`
	HTMLURL    string   `json:"html_url"`
	Reporter   string   `json:"reporter"`
}

type Stats struct {
	Exchanges   int `json:"exchanges"`
	Refreshes   int `json:"refreshes"`
	RateLimited int `json:"rate_limited"`
	Issues      int `json:"issues"`
}

// grant is one authorization of the consumer by a user.
type grant struct {
	user    int
	number  int // the user's grant counter, for the refresh token
	seq     int // the user's access-token counter
	expiry  time.Time
	revoked bool
}

func (g *grant) access() string  { return fmt.Sprintf("bb-access-%d-%d", g.user, g.seq) }
func (g *grant) refresh() string { return fmt.Sprintf("bb-refresh-%d-%d", g.user, g.number) }

type Server struct {
	publicURL string

	mu          sync.Mutex
	grants      []*grant
	seq         map[int]int // access tokens issued per user
	numbers     map[int]int // grants per user
	issues      []*Issue
	limitedSeen map[string]bool
	stats       Stats
}

// New returns a fake whose issue URLs start with publicURL.
func New(publicURL string) *Server {
	return &Server{
		publicURL:   strings.TrimRight(publicURL, "/"),
		seq:         map[int]int{},
		numbers:     map[int]int{},
		limitedSeen: map[string]bool{},
	}
}

func (s *Server) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.Issues = len(s.issues)
	return st
}

func (s *Server) Issues() []Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Issue, len(s.issues))
	for i, is := range s.issues {
		out[i] = *is
	}
	return out
}

// Revoke makes every token of user N invalid.
func (s *Server) Revoke(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.grants {
		if g.user == id {
			g.revoked = true
		}
	}
}

// issue gives g its next access token.
func (s *Server) issue(g *grant, lifetime time.Duration) {
	s.seq[g.user]++
	g.seq = s.seq[g.user]
	g.expiry = time.Now().Add(lifetime)
}

// SetState changes an issue's state, for tests.
func (s *Server) SetState(repo string, id int, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, is := range s.issues {
		if is.Repository == repo && is.ID == id {
			is.State = state
		}
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /site/oauth2/authorize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "fakebitbucket authorize page for consumer %q.\n\nFinish by calling the callback with ?code=%sN&state=%s\n",
			r.URL.Query().Get("client_id"), CodePrefix, r.URL.Query().Get("state"))
	})
	mux.HandleFunc("POST /site/oauth2/access_token", s.token)

	api := http.NewServeMux()
	api.HandleFunc("GET /2.0/user", s.currentUser)
	api.HandleFunc("GET /2.0/repositories", s.listRepositories)
	api.HandleFunc("GET /2.0/repositories/{ws}/{slug}", s.getRepository)
	api.HandleFunc("GET /2.0/repositories/{ws}/{slug}/milestones", s.milestones)
	api.HandleFunc("POST /2.0/repositories/{ws}/{slug}/issues", s.createIssue)
	api.HandleFunc("GET /2.0/repositories/{ws}/{slug}/issues/{id}", s.getIssue)
	api.HandleFunc("POST /2.0/repositories/{ws}/{slug}/issues/{id}/comments", s.createComment)
	mux.Handle("/2.0/", s.requireToken(api))

	mux.HandleFunc("GET /_fake/issues", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, s.Issues()) })
	mux.HandleFunc("DELETE /_fake/issues", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		s.issues = nil
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /_fake/stats", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, s.Stats()) })
	mux.HandleFunc("POST /_fake/revoke", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("user"))
		if err != nil {
			http.Error(w, "user=N required", http.StatusBadRequest)
			return
		}
		s.Revoke(id)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok || id != ClientID || secret != ClientSecret {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "Invalid OAuth client credentials")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "unreadable form")
		return
	}
	f := r.PostForm
	s.mu.Lock()
	defer s.mu.Unlock()

	switch f.Get("grant_type") {
	case "authorization_code":
		rest, ok := strings.CutPrefix(f.Get("code"), CodePrefix)
		short := false
		if r, cut := strings.CutSuffix(rest, "-short"); cut {
			rest, short = r, true
		}
		n, err := strconv.Atoi(rest)
		if !ok || err != nil || n <= 0 {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "The specified code is not valid.")
			return
		}
		lifetime := 2 * time.Hour
		if short {
			lifetime = time.Second
		}
		s.numbers[n]++
		g := &grant{user: n, number: s.numbers[n]}
		s.issue(g, lifetime)
		s.grants = append(s.grants, g)
		s.stats.Exchanges++
		writeToken(w, g, lifetime)

	case "refresh_token":
		for _, g := range s.grants {
			if !g.revoked && f.Get("refresh_token") == g.refresh() {
				s.issue(g, 2*time.Hour)
				s.stats.Refreshes++
				writeToken(w, g, 2*time.Hour)
				return
			}
		}
		oauthError(w, http.StatusBadRequest, "invalid_grant", "Invalid refresh_token")

	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "")
	}
}

func writeToken(w http.ResponseWriter, g *grant, lifetime time.Duration) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  g.access(),
		"refresh_token": g.refresh(),
		"token_type":    "bearer",
		"expires_in":    int(lifetime.Seconds()),
		"scopes":        "account repository issue:write",
	})
}

func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		valid := 0
		for _, g := range s.grants {
			if tok == g.access() && !g.revoked && time.Now().Before(g.expiry) {
				valid = g.user
			}
		}
		s.mu.Unlock()
		if valid == 0 {
			apiError(w, http.StatusUnauthorized, "Access token expired or invalid.")
			return
		}
		r.Header.Set("X-Fake-User", strconv.Itoa(valid))
		next.ServeHTTP(w, r)
	})
}

func userN(r *http.Request) int {
	n, _ := strconv.Atoi(r.Header.Get("X-Fake-User"))
	return n
}

func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) {
	n := userN(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"type": "user", "uuid": UserUUID(n),
		"username": fmt.Sprintf("bb-user-%d", n), "nickname": fmt.Sprintf("bb-user-%d", n),
		"display_name": fmt.Sprintf("Bitbucket User %d", n), "account_id": fmt.Sprintf("bb-account-%d", n),
	})
}

func (s *Server) repoJSON(full string) map[string]any {
	return map[string]any{
		"type":       "repository",
		"full_name":  full,
		"is_private": true,
		"has_issues": full != NoIssuesRepo,
		"links":      map[string]any{"html": map[string]string{"href": s.publicURL + "/" + full}},
	}
}

// listRepositories pages like Bitbucket: pagelen, page and an absolute
// "next" link on the host the request came to.
func (s *Server) listRepositories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("role") != "member" {
		apiError(w, http.StatusBadRequest, "the fake only lists role=member")
		return
	}
	per, _ := strconv.Atoi(q.Get("pagelen"))
	if per <= 0 {
		per = 10
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	values := []any{}
	start := (page - 1) * per
	for i := start; i < len(Repositories) && i < start+per; i++ {
		values = append(values, s.repoJSON(Repositories[i]))
	}
	body := map[string]any{"pagelen": per, "page": page, "size": len(Repositories), "values": values}
	if start+per < len(Repositories) {
		body["next"] = fmt.Sprintf("http://%s/2.0/repositories?role=member&pagelen=%d&page=%d", r.Host, per, page+1)
	}
	writeJSON(w, http.StatusOK, body)
}

func repo(r *http.Request) (string, bool) {
	full := r.PathValue("ws") + "/" + r.PathValue("slug")
	return full, slices.Contains(Repositories, full)
}

func (s *Server) getRepository(w http.ResponseWriter, r *http.Request) {
	full, ok := repo(r)
	if !ok {
		apiError(w, http.StatusNotFound, "Repository "+full+" not found")
		return
	}
	writeJSON(w, http.StatusOK, s.repoJSON(full))
}

func (s *Server) milestones(w http.ResponseWriter, r *http.Request) {
	full, ok := repo(r)
	if !ok || full == NoIssuesRepo {
		apiError(w, http.StatusNotFound, "Repository has no issue tracker.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": []any{map[string]any{"id": 1, "name": "v1.0"}}})
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	full, ok := repo(r)
	if !ok {
		apiError(w, http.StatusNotFound, "Repository "+full+" not found")
		return
	}
	if full == NoIssuesRepo {
		apiError(w, http.StatusNotFound, "Repository has no issue tracker.")
		return
	}
	var req struct {
		Title   string `json:"title"`
		Content struct {
			Raw string `json:"raw"`
		} `json:"content"`
		Kind      string `json:"kind"`
		Priority  string `json:"priority"`
		Milestone *struct {
			Name string `json:"name"`
		} `json:"milestone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		apiError(w, http.StatusBadRequest, "title: This field is required.")
		return
	}
	milestone := ""
	if req.Milestone != nil {
		if req.Milestone.Name != "v1.0" {
			apiError(w, http.StatusBadRequest, "milestone: No such milestone")
			return
		}
		milestone = req.Milestone.Name
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch full {
	case BrokenRepo:
		apiError(w, http.StatusInternalServerError, "Something went wrong (simulated by fakebitbucket)")
		return
	case ExhaustedRepo:
		s.stats.RateLimited++
		w.Header().Set("Retry-After", "3600")
		apiError(w, http.StatusTooManyRequests, "Rate limit for this resource has been exceeded")
		return
	case LimitedRepo:
		if !s.limitedSeen[req.Title] {
			s.limitedSeen[req.Title] = true
			s.stats.RateLimited++
			w.Header().Set("Retry-After", "1")
			apiError(w, http.StatusTooManyRequests, "Rate limit for this resource has been exceeded")
			return
		}
	}

	id := 1
	for _, is := range s.issues {
		if is.Repository == full && is.ID >= id {
			id = is.ID + 1
		}
	}
	is := &Issue{
		Repository: full, ID: id, Title: req.Title, Content: req.Content.Raw,
		Kind: req.Kind, Priority: req.Priority, Milestone: milestone,
		State: "new", Comments: []string{}, Reporter: UserUUID(userN(r)),
		HTMLURL: fmt.Sprintf("%s/%s/issues/%d", s.publicURL, full, id),
	}
	s.issues = append(s.issues, is)
	writeJSON(w, http.StatusCreated, issueJSON(is))
}

func (s *Server) findIssue(r *http.Request) *Issue {
	full, ok := repo(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if !ok || err != nil {
		return nil
	}
	for _, is := range s.issues {
		if is.Repository == full && is.ID == id {
			return is
		}
	}
	return nil
}

func (s *Server) getIssue(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	is := s.findIssue(r)
	if is == nil {
		apiError(w, http.StatusNotFound, "Issue not found")
		return
	}
	writeJSON(w, http.StatusOK, issueJSON(is))
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content struct {
			Raw string `json:"raw"`
		} `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content.Raw == "" {
		apiError(w, http.StatusBadRequest, "content: This field is required.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	is := s.findIssue(r)
	if is == nil {
		apiError(w, http.StatusNotFound, "Issue not found")
		return
	}
	is.Comments = append(is.Comments, req.Content.Raw)
	writeJSON(w, http.StatusCreated, map[string]any{"id": len(is.Comments)})
}

func issueJSON(is *Issue) map[string]any {
	return map[string]any{
		"type": "issue", "id": is.ID, "title": is.Title, "state": is.State,
		"links": map[string]any{"html": map[string]string{"href": is.HTMLURL}},
	}
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// apiError answers in Bitbucket's error format.
func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]string{"message": msg}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
