// Package fakegitlab is a development and test stand-in for gitlab.com: the
// OAuth endpoints and the handful of REST API v4 endpoints the gitlab
// provider calls. The provider's unit tests run it in-process and
// cmd/fakegitlab serves it in the dev stack.
//
// Fixtures:
//
//	client             fake-client / fake-secret (in the form body, like GitLab)
//	OAuth code         fake-code-<N>        → user N, tokens valid 2 h
//	                   fake-code-<N>-short  → user N, access token valid 1 s
//	                                          (forces a refresh on first use)
//	user N             id N, username gl-user-<N>
//	tokens             access gl-access-<N>-<S>, refresh gl-refresh-<N>-<S>,
//	                   where S counts the tokens issued to user N. Every
//	                   code exchange is a separate grant (two apps
//	                   authorised by the same user each keep working
//	                   tokens). Like GitLab, a refresh token works once
//	                   (rotation) and replaces its grant's access token.
//	projects           seismo-group/seismo-test, seismo-group/another,
//	                   seismo-group/sub/nested,
//	                   seismo-group/broken     (issue creation: 500)
//	                   seismo-group/limited    (first create per title: 429, Retry-After 1)
//	                   seismo-group/exhausted  (issue creation: 429, Retry-After 3600)
//	milestone          "v1.0" (id 1) in every project
//
// Inspection endpoints:
//
//	GET    /_fake/issues        every issue opened
//	DELETE /_fake/issues        forget them
//	GET    /_fake/stats         exchanges, refreshes, rate_limited, issues
//	POST   /_fake/revoke?user=N revoke user N's tokens (as if the user
//	                            revoked the application)
package fakegitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	Group        = "seismo-group"

	BrokenProject    = Group + "/broken"
	LimitedProject   = Group + "/limited"
	ExhaustedProject = Group + "/exhausted"
)

// Projects every user can see, in listing order.
var Projects = []string{
	Group + "/seismo-test", Group + "/another", Group + "/sub/nested",
	BrokenProject, LimitedProject, ExhaustedProject,
}

// Issue is an issue opened on the fake.
type Issue struct {
	Project     string   `json:"project"`
	IID         int      `json:"iid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	MilestoneID *int     `json:"milestone_id"`
	AssigneeIDs []int    `json:"assignee_ids"`
	State       string   `json:"state"`
	Notes       []string `json:"notes"`
	WebURL      string   `json:"web_url"`
	AuthorID    int      `json:"author_id"`
}

// Stats counts what the fake was asked to do.
type Stats struct {
	Exchanges   int `json:"exchanges"`
	Refreshes   int `json:"refreshes"`
	RateLimited int `json:"rate_limited"`
	Issues      int `json:"issues"`
}

// grant is one authorization of the application by a user: one code
// exchange and the refreshes that follow it.
type grant struct {
	user    int
	seq     int
	expiry  time.Time
	revoked bool
}

func (g *grant) access() string  { return fmt.Sprintf("gl-access-%d-%d", g.user, g.seq) }
func (g *grant) refresh() string { return fmt.Sprintf("gl-refresh-%d-%d", g.user, g.seq) }

// Server is the fake. Create it with New and serve Handler().
type Server struct {
	publicURL string

	mu          sync.Mutex
	grants      []*grant
	seq         map[int]int // tokens issued per user
	issues      []*Issue
	limitedSeen map[string]bool
	stats       Stats
}

// New returns a fake whose issue URLs start with publicURL.
func New(publicURL string) *Server {
	return &Server{
		publicURL:   strings.TrimRight(publicURL, "/"),
		seq:         map[int]int{},
		limitedSeen: map[string]bool{},
	}
}

// Stats returns a copy of the counters.
func (s *Server) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.Issues = len(s.issues)
	return st
}

// Issues returns a copy of the opened issues.
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

// issue gives g its next pair of tokens.
func (s *Server) issue(g *grant, lifetime time.Duration) {
	s.seq[g.user]++
	g.seq = s.seq[g.user]
	g.expiry = time.Now().Add(lifetime)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /oauth/authorize", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "fakegitlab authorize page.\n\nFinish by calling redirect_uri?code=%sN&state=<the state of this URL>\n", CodePrefix)
	})
	mux.HandleFunc("POST /oauth/token", s.token)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v4/user", s.currentUser)
	api.HandleFunc("GET /api/v4/users", s.findUsers)
	api.HandleFunc("GET /api/v4/projects", s.listProjects)
	api.HandleFunc("GET /api/v4/projects/{id}", s.getProject)
	api.HandleFunc("GET /api/v4/projects/{id}/milestones", s.milestones)
	api.HandleFunc("POST /api/v4/projects/{id}/issues", s.createIssue)
	api.HandleFunc("GET /api/v4/projects/{id}/issues/{iid}", s.getIssue)
	api.HandleFunc("POST /api/v4/projects/{id}/issues/{iid}/notes", s.createNote)
	mux.Handle("/api/v4/", s.requireToken(api))

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

// token implements POST /oauth/token for both grants.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "unreadable form")
		return
	}
	f := r.PostForm
	if f.Get("client_id") != ClientID || f.Get("client_secret") != ClientSecret {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	switch f.Get("grant_type") {
	case "authorization_code":
		if f.Get("redirect_uri") == "" {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri is required")
			return
		}
		rest, ok := strings.CutPrefix(f.Get("code"), CodePrefix)
		short := false
		if r, cut := strings.CutSuffix(rest, "-short"); cut {
			rest, short = r, true
		}
		id, err := strconv.Atoi(rest)
		if !ok || err != nil || id <= 0 {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "The provided authorization grant is invalid")
			return
		}
		lifetime := 2 * time.Hour
		if short {
			lifetime = time.Second
		}
		g := &grant{user: id}
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
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The provided authorization grant is invalid, expired, revoked, or was issued to another client")

	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "")
	}
}

func writeToken(w http.ResponseWriter, g *grant, lifetime time.Duration) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  g.access(),
		"refresh_token": g.refresh(),
		"token_type":    "Bearer",
		"expires_in":    int(lifetime.Seconds()),
		"scope":         "api",
		"created_at":    time.Now().Unix(),
	})
}

// requireToken accepts a grant's current, unexpired, unrevoked access
// token, and adds GitLab's rate-limit headers.
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
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "401 Unauthorized"})
			return
		}
		w.Header().Set("RateLimit-Limit", "2000")
		w.Header().Set("RateLimit-Remaining", "1999")
		r.Header.Set("X-Fake-User", strconv.Itoa(valid))
		next.ServeHTTP(w, r)
	})
}

func userID(r *http.Request) int {
	id, _ := strconv.Atoi(r.Header.Get("X-Fake-User"))
	return id
}

func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) {
	id := userID(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "username": fmt.Sprintf("gl-user-%d", id), "name": fmt.Sprintf("GitLab User %d", id),
		"state": "active", "web_url": fmt.Sprintf("%s/gl-user-%d", s.publicURL, id),
	})
}

// findUsers resolves gl-user-N usernames, for assignees.
func (s *Server) findUsers(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("username")
	out := []any{}
	if rest, ok := strings.CutPrefix(name, "gl-user-"); ok {
		if id, err := strconv.Atoi(rest); err == nil && id > 0 {
			out = append(out, map[string]any{"id": id, "username": name})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) projectJSON(i int, path string) map[string]any {
	return map[string]any{
		"id":                  100 + i,
		"path_with_namespace": path,
		"visibility":          "private",
		"web_url":             s.publicURL + "/" + path,
	}
}

// listProjects pages like GitLab: per_page, page, X-Next-Page.
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("membership") != "true" {
		http.Error(w, `{"message":"the fake only lists membership=true"}`, http.StatusBadRequest)
		return
	}
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per <= 0 {
		per = 20
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	out := []any{}
	start := (page - 1) * per
	for i := start; i < len(Projects) && i < start+per; i++ {
		out = append(out, s.projectJSON(i, Projects[i]))
	}
	if start+per < len(Projects) {
		w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
	} else {
		w.Header().Set("X-Next-Page", "")
	}
	w.Header().Set("X-Total", strconv.Itoa(len(Projects)))
	writeJSON(w, http.StatusOK, out)
}

// project resolves {id}: a numeric ID or a URL-encoded path, as on GitLab.
func project(r *http.Request) (string, int, bool) {
	raw := r.PathValue("id")
	if n, err := strconv.Atoi(raw); err == nil {
		if i := n - 100; i >= 0 && i < len(Projects) {
			return Projects[i], i, true
		}
		return "", 0, false
	}
	path, err := url.PathUnescape(raw)
	if err != nil {
		return "", 0, false
	}
	i := slices.Index(Projects, path)
	return path, i, i >= 0
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	path, i, ok := project(r)
	if !ok {
		notFound(w, "404 Project Not Found")
		return
	}
	writeJSON(w, http.StatusOK, s.projectJSON(i, path))
}

func (s *Server) milestones(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := project(r); !ok {
		notFound(w, "404 Project Not Found")
		return
	}
	out := []any{}
	if t := r.URL.Query().Get("title"); t == "" || t == "v1.0" {
		out = append(out, map[string]any{"id": 1, "iid": 1, "title": "v1.0", "state": "active"})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	path, _, ok := project(r)
	if !ok {
		notFound(w, "404 Project Not Found")
		return
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Labels      string `json:"labels"`
		MilestoneID *int   `json:"milestone_id"`
		AssigneeIDs []int  `json:"assignee_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": map[string][]string{"title": {"can't be blank"}}})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch path {
	case BrokenProject:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "500 Internal Server Error (simulated by fakegitlab)"})
		return
	case ExhaustedProject:
		s.stats.RateLimited++
		w.Header().Set("Retry-After", "3600")
		w.Header().Set("RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "Retry later"})
		return
	case LimitedProject:
		if !s.limitedSeen[req.Title] {
			s.limitedSeen[req.Title] = true
			s.stats.RateLimited++
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "Retry later"})
			return
		}
	}

	iid := 1
	for _, is := range s.issues {
		if is.Project == path && is.IID >= iid {
			iid = is.IID + 1
		}
	}
	labels := []string{}
	if req.Labels != "" {
		labels = strings.Split(req.Labels, ",")
	}
	is := &Issue{
		Project: path, IID: iid, Title: req.Title, Description: req.Description,
		Labels: labels, MilestoneID: req.MilestoneID, AssigneeIDs: req.AssigneeIDs,
		State: "opened", Notes: []string{}, AuthorID: userID(r),
		WebURL: fmt.Sprintf("%s/%s/-/issues/%d", s.publicURL, path, iid),
	}
	s.issues = append(s.issues, is)
	writeJSON(w, http.StatusCreated, issueJSON(is))
}

func (s *Server) findIssue(r *http.Request) *Issue {
	path, _, ok := project(r)
	iid, err := strconv.Atoi(r.PathValue("iid"))
	if !ok || err != nil {
		return nil
	}
	for _, is := range s.issues {
		if is.Project == path && is.IID == iid {
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
		notFound(w, "404 Not found")
		return
	}
	writeJSON(w, http.StatusOK, issueJSON(is))
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "body is missing"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	is := s.findIssue(r)
	if is == nil {
		notFound(w, "404 Not found")
		return
	}
	is.Notes = append(is.Notes, req.Body)
	writeJSON(w, http.StatusCreated, map[string]any{"id": len(is.Notes), "body": req.Body})
}

// SetState closes or reopens an issue, for tests.
func (s *Server) SetState(project string, iid int, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, is := range s.issues {
		if is.Project == project && is.IID == iid {
			is.State = state
		}
	}
}

func issueJSON(is *Issue) map[string]any {
	return map[string]any{"iid": is.IID, "title": is.Title, "state": is.State, "web_url": is.WebURL, "labels": is.Labels}
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func notFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
