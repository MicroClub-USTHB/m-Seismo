package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apps"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpapi"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/projects"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/testutil"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/users"
)

const token = "test-admin-token-0123456789abcdef0123"

type client struct {
	t    *testing.T
	srv  *httptest.Server
	pool *pgxpool.Pool
}

func newClient(t *testing.T) *client {
	t.Helper()
	pool := testutil.NewDB(t)
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{
		AdminToken:     token,
		RequestTimeout: 10 * time.Second,
		Ready:          pool.Ping,
		Users:          users.NewService(pool),
		Projects:       projects.NewService(pool),
		Apps:           apps.NewService(pool),
	}))
	t.Cleanup(srv.Close)
	return &client{t: t, srv: srv, pool: pool}
}

// do sends body (marshalled unless it is a string) and decodes the response
// into out when out is non-nil. It returns the status code.
func (c *client) do(method, path string, body, out any) int {
	c.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = bytes.NewBufferString(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.srv.URL+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if r != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			c.t.Fatalf("%s %s: decode response: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (c *client) mustStatus(want int, method, path string, body, out any) {
	c.t.Helper()
	if got := c.do(method, path, body, out); got != want {
		c.t.Fatalf("%s %s: status %d, want %d", method, path, got, want)
	}
}

func (c *client) auditCount(action string) int {
	c.t.Helper()
	var n int
	if err := c.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_logs WHERE action = $1`, action).Scan(&n); err != nil {
		c.t.Fatal(err)
	}
	return n
}

type problem struct {
	Status        int `json:"status"`
	InvalidParams []struct {
		Name string `json:"name"`
	} `json:"invalid-params"`
}

func TestAuthRequired(t *testing.T) {
	c := newClient(t)
	for _, auth := range []string{"", "Bearer wrong", token} {
		req, _ := http.NewRequest(http.MethodGet, c.srv.URL+"/v1/users", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status %d, want 401", auth, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("content type %q", ct)
		}
	}

	resp, err := http.Get(c.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("healthz: status %d", resp.StatusCode)
	}
}

func TestUsersCRUD(t *testing.T) {
	c := newClient(t)

	var u users.User
	c.mustStatus(201, "POST", "/v1/users", map[string]any{"name": " Wail ", "email": "wail@example.com"}, &u)
	if u.Name != "Wail" || u.Email == nil || *u.Email != "wail@example.com" {
		t.Fatalf("created user = %+v", u)
	}
	if n := c.auditCount("user.create"); n != 1 {
		t.Errorf("user.create audit rows = %d, want 1", n)
	}

	// Email uniqueness is case-insensitive.
	c.mustStatus(409, "POST", "/v1/users", map[string]any{"name": "Other", "email": "WAIL@example.com"}, nil)

	var p problem
	c.mustStatus(422, "POST", "/v1/users", map[string]any{"name": "", "email": "nope"}, &p)
	if len(p.InvalidParams) != 2 {
		t.Errorf("invalid params = %+v, want name and email", p.InvalidParams)
	}
	c.mustStatus(400, "POST", "/v1/users", `{"name":"x","admin":true}`, nil)

	var got users.User
	c.mustStatus(200, "GET", "/v1/users/"+u.ID.String(), nil, &got)
	if got.ID != u.ID {
		t.Errorf("get returned %s", got.ID)
	}
	c.mustStatus(404, "GET", "/v1/users/00000000-0000-0000-0000-000000000000", nil, nil)
	c.mustStatus(404, "GET", "/v1/users/not-a-uuid", nil, nil)

	// PATCH: absent fields are kept, null clears the email.
	c.mustStatus(200, "PATCH", "/v1/users/"+u.ID.String(), `{"email":null}`, &got)
	if got.Email != nil || got.Name != "Wail" {
		t.Errorf("after clearing email: %+v", got)
	}
	c.mustStatus(200, "PATCH", "/v1/users/"+u.ID.String(), `{"name":"Wail S"}`, &got)
	if got.Name != "Wail S" || !got.UpdatedAt.After(got.CreatedAt) {
		t.Errorf("after rename: %+v", got)
	}
	c.mustStatus(422, "PATCH", "/v1/users/"+u.ID.String(), `{"name":null}`, nil)

	c.mustStatus(204, "DELETE", "/v1/users/"+u.ID.String(), nil, nil)
	c.mustStatus(404, "DELETE", "/v1/users/"+u.ID.String(), nil, nil)
	if n := c.auditCount("user.delete"); n != 1 {
		t.Errorf("user.delete audit rows = %d, want 1", n)
	}
}

func TestUsersPagination(t *testing.T) {
	c := newClient(t)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		c.mustStatus(201, "POST", "/v1/users", map[string]any{"name": name}, nil)
	}
	var seen []string
	cursor := ""
	for range 10 {
		var page struct {
			Items      []users.User `json:"items"`
			NextCursor string       `json:"next_cursor"`
		}
		c.mustStatus(200, "GET", "/v1/users?limit=2&cursor="+cursor, nil, &page)
		for _, u := range page.Items {
			seen = append(seen, u.Name)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("paged through %v, want 5 users", seen)
	}
	c.mustStatus(400, "GET", "/v1/users?cursor=garbage", nil, nil)
	c.mustStatus(400, "GET", "/v1/users?limit=0", nil, nil)
}

func TestProjectsAndAppsCRUD(t *testing.T) {
	c := newClient(t)

	var owner, other users.User
	c.mustStatus(201, "POST", "/v1/users", map[string]any{"name": "owner"}, &owner)
	c.mustStatus(201, "POST", "/v1/users", map[string]any{"name": "other"}, &other)
	base := "/v1/users/" + owner.ID.String() + "/projects"

	var p projects.Project
	c.mustStatus(201, "POST", base, map[string]any{
		"name": "Club App", "settings": map[string]any{"rate_limit_per_minute": 60},
	}, &p)
	if p.Slug != "club-app" || p.Settings.RateLimitPerMinute == nil || *p.Settings.RateLimitPerMinute != 60 {
		t.Fatalf("created project = %+v", p)
	}
	c.mustStatus(409, "POST", base, map[string]any{"name": "Club   App!"}, nil)
	c.mustStatus(422, "POST", base, map[string]any{"name": "x", "slug": "Bad Slug"}, nil)
	c.mustStatus(404, "POST", "/v1/users/00000000-0000-0000-0000-000000000000/projects", map[string]any{"name": "x"}, nil)

	// Another user cannot see or touch the project.
	otherProject := "/v1/users/" + other.ID.String() + "/projects/" + p.ID.String()
	c.mustStatus(404, "GET", otherProject, nil, nil)
	c.mustStatus(404, "PATCH", otherProject, `{"name":"stolen"}`, nil)
	c.mustStatus(404, "DELETE", otherProject, nil, nil)
	c.mustStatus(404, "POST", otherProject+"/apps", map[string]any{"name": "x", "platform": "ios", "bundle_id": "x"}, nil)

	projectPath := base + "/" + p.ID.String()
	var reset projects.Project
	c.mustStatus(200, "PATCH", projectPath, `{"settings":null}`, &reset)
	if reset.Settings != (projects.Settings{}) || reset.Name != "Club App" {
		t.Errorf("after settings reset: %+v", reset)
	}

	var a apps.App
	c.mustStatus(201, "POST", projectPath+"/apps", map[string]any{
		"name": "Club iOS", "platform": "ios", "bundle_id": "dev.microclub.app",
	}, &a)
	if a.Environment != "production" || a.ProjectID != p.ID {
		t.Fatalf("created app = %+v", a)
	}
	c.mustStatus(409, "POST", projectPath+"/apps", map[string]any{
		"name": "dup", "platform": "android", "bundle_id": "dev.microclub.app",
	}, nil)
	c.mustStatus(201, "POST", projectPath+"/apps", map[string]any{
		"name": "Club iOS staging", "platform": "ios", "bundle_id": "dev.microclub.app", "environment": "staging",
	}, nil)

	var prob problem
	c.mustStatus(422, "POST", projectPath+"/apps", map[string]any{"name": "x", "platform": "windows", "bundle_id": ""}, &prob)
	if len(prob.InvalidParams) != 2 {
		t.Errorf("invalid params = %+v", prob.InvalidParams)
	}

	appPath := projectPath + "/apps/" + a.ID.String()
	c.mustStatus(404, "GET", otherProject+"/apps/"+a.ID.String(), nil, nil)
	c.mustStatus(200, "PATCH", appPath, `{"platform":"flutter"}`, &a)
	if a.Platform != apps.PlatformFlutter || a.Name != "Club iOS" {
		t.Errorf("after patch: %+v", a)
	}

	var list struct {
		Items []apps.App `json:"items"`
	}
	c.mustStatus(200, "GET", projectPath+"/apps", nil, &list)
	if len(list.Items) != 2 {
		t.Errorf("listed %d apps, want 2", len(list.Items))
	}

	c.mustStatus(204, "DELETE", appPath, nil, nil)
	c.mustStatus(404, "GET", appPath, nil, nil)

	// Deleting the user cascades to its projects and apps.
	c.mustStatus(204, "DELETE", "/v1/users/"+owner.ID.String(), nil, nil)
	var remaining int
	if err := c.pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM projects) + (SELECT count(*) FROM apps)`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Errorf("%d projects/apps left after cascading delete", remaining)
	}
}
