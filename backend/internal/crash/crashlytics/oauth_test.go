package crashlytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// memStates is an in-memory States for tests.
type memStates map[string]string

func (s memStates) Put(_ context.Context, state, projectID string, _ time.Duration) error {
	s[state] = projectID
	return nil
}

func (s memStates) Take(_ context.Context, state string) (string, bool, error) {
	id, ok := s[state]
	delete(s, state)
	return id, ok, nil
}

// fakeGoogle serves the token and userinfo endpoints.
func fakeGoogle(t *testing.T, refreshToken string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != "good-code" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": refreshToken,
			})
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer access" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"email": "bot@example.com"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// tokens maps the bearer tokens the tests send to owners.
type tokens map[string]uuid.UUID

func (t tokens) Verify(token string) (uuid.UUID, error) {
	id, ok := t[token]
	if !ok {
		return uuid.Nil, errors.New("unknown token")
	}
	return id, nil
}

type oauthTest struct {
	store  *memStore
	states memStates
	router chi.Router
	// token is sent as the bearer token; empty sends none.
	token string
}

func newOAuthTest(t *testing.T, refreshToken string, conns ...Connection) *oauthTest {
	t.Helper()
	google := fakeGoogle(t, refreshToken)

	ot := &oauthTest{store: newMemStore(conns...), states: memStates{}, router: chi.NewRouter(), token: "owner"}
	require := httpx.NewAuthMiddleware(tokens{"owner": testOwner, "other": otherOwner})
	h := NewHandler(testConfig, ot.store, ot.states, require)
	h.oauth.Endpoint = oauth2.Endpoint{AuthURL: "https://accounts.google.test/auth", TokenURL: google.URL + "/token"}
	h.userinfoURL = google.URL + "/userinfo"
	h.httpClient = google.Client()

	ot.router.Route("/api/v1", h.Mount)
	return ot
}

func (ot *oauthTest) do(method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api"+target, strings.NewReader(body))
	if ot.token != "" {
		req.Header.Set("Authorization", "Bearer "+ot.token)
	}
	rec := httptest.NewRecorder()
	ot.router.ServeHTTP(rec, req)
	return rec
}

func TestConnectFlow(t *testing.T) {
	ot := newOAuthTest(t, "refresh")

	rec := ot.do("POST", "/v1/projects/"+project+"/crashlytics/connect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("connect: status %d: %s", rec.Code, rec.Body)
	}
	var resp struct{ URL string }
	json.NewDecoder(rec.Body).Decode(&resp)
	consent, err := url.Parse(resp.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := consent.Query()
	if q.Get("access_type") != "offline" || q.Get("prompt") != "consent" || q.Get("redirect_uri") != testConfig.RedirectURL {
		t.Errorf("consent URL missing offline access, consent prompt or redirect: %s", resp.URL)
	}
	if !strings.Contains(q.Get("scope"), "firebase") || !strings.Contains(q.Get("scope"), "email") {
		t.Errorf("consent URL scopes = %q", q.Get("scope"))
	}
	state := q.Get("state")
	if ot.states[state] != project {
		t.Fatalf("state %q not stored for the project", state)
	}

	rec = ot.do("GET", "/v1/oauth/crashlytics/callback?code=good-code&state="+state, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("callback: status %d: %s", rec.Code, rec.Body)
	}
	conn := ot.store.conns[project]
	if conn.RefreshToken != "refresh" || conn.Email != "bot@example.com" {
		t.Errorf("saved connection = %+v", conn)
	}

	// The state works only once.
	rec = ot.do("GET", "/v1/oauth/crashlytics/callback?code=good-code&state="+state, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("reused state: status %d, want 400", rec.Code)
	}
}

func TestConnectUnknownProject(t *testing.T) {
	ot := newOAuthTest(t, "refresh")
	rec := ot.do("POST", "/v1/projects/unknown/crashlytics/connect", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

func TestProjectEndpointsNeedTheOwner(t *testing.T) {
	ot := newOAuthTest(t, "refresh", Connection{ProjectID: project, Email: "bot@example.com", RefreshToken: "refresh"})
	routes := []struct{ method, target, body string }{
		{"POST", "/v1/projects/" + project + "/crashlytics/connect", ""},
		{"GET", "/v1/projects/" + project + "/crashlytics", ""},
		{"PUT", "/v1/projects/" + project + "/crashlytics/apps", `{"apps":["` + app + `"]}`},
	}
	for _, rt := range routes {
		ot.token = ""
		if rec := ot.do(rt.method, rt.target, rt.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", rt.method, rt.target, rec.Code)
		}
		// Another owner's project looks like one that does not exist.
		ot.token = "other"
		if rec := ot.do(rt.method, rt.target, rt.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as another owner: status %d, want 404", rt.method, rt.target, rec.Code)
		}
	}
	if len(ot.states) != 0 || ot.store.conns[project].Apps != nil {
		t.Error("a refused request changed state")
	}
}

func TestCallbackFailures(t *testing.T) {
	tests := []struct {
		name         string
		refreshToken string
		query        string
		want         int
	}{
		{name: "unknown state", refreshToken: "refresh", query: "code=good-code&state=forged", want: http.StatusBadRequest},
		{name: "access denied", refreshToken: "refresh", query: "error=access_denied&state=s", want: http.StatusBadRequest},
		{name: "bad code", refreshToken: "refresh", query: "code=bad&state=s", want: http.StatusBadGateway},
		{name: "no refresh token", refreshToken: "", query: "code=good-code&state=s", want: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ot := newOAuthTest(t, tt.refreshToken)
			ot.states["s"] = project

			rec := ot.do("GET", "/v1/oauth/crashlytics/callback?"+tt.query, "")
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			if _, ok := ot.store.conns[project]; ok {
				t.Error("connection saved on failure")
			}
		})
	}
}

func TestStatusAndApps(t *testing.T) {
	ot := newOAuthTest(t, "refresh")

	rec := ot.do("GET", "/v1/projects/"+project+"/crashlytics", "")
	if !strings.Contains(rec.Body.String(), `"connected":false`) {
		t.Errorf("status before connecting = %s", rec.Body)
	}

	rec = ot.do("PUT", "/v1/projects/"+project+"/crashlytics/apps", `{"apps":["`+app+`"]}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("set apps before connecting: status %d, want 409", rec.Code)
	}

	ot.store.Save(context.Background(), project, "bot@example.com", "refresh")

	rec = ot.do("PUT", "/v1/projects/"+project+"/crashlytics/apps", `{"apps":["not-an-app"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid app: status %d, want 400", rec.Code)
	}

	rec = ot.do("PUT", "/v1/projects/"+project+"/crashlytics/apps", `{"apps":["`+app+`"]}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("set apps: status %d: %s", rec.Code, rec.Body)
	}

	rec = ot.do("GET", "/v1/projects/"+project+"/crashlytics", "")
	body := rec.Body.String()
	if !strings.Contains(body, `"connected":true`) || !strings.Contains(body, app) || !strings.Contains(body, "bot@example.com") {
		t.Errorf("status = %s", body)
	}
	if strings.Contains(body, "refresh") {
		t.Error("status leaks the refresh token")
	}
}
