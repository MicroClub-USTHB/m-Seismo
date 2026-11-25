package crashlytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/option"
)

// fakeFirebase serves the Firebase Management API calls that list apps.
func fakeFirebase(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1beta1/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"projectId": "demo", "projectNumber": "123456", "displayName": "Demo"},
			}})
		case "/v1beta1/projects/demo:searchApps":
			_ = json.NewEncoder(w).Encode(map[string]any{"apps": []map[string]any{
				{"appId": "1:123456:android:abcd", "displayName": "Demo Android", "platform": "ANDROID", "namespace": "com.example.demo", "state": "ACTIVE"},
				{"appId": "1:123456:ios:dead", "platform": "IOS", "state": "DELETED"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFirebaseApps(t *testing.T) {
	ot := newOAuthTest(t, "refresh")
	fb := fakeFirebase(t)
	ot.handler.firebaseOpts = []option.ClientOption{option.WithEndpoint(fb.URL + "/"), option.WithHTTPClient(fb.Client())}

	rec := ot.do("GET", "/v1/projects/"+project+"/crashlytics/firebase-apps", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("before connecting: status %d, want 409", rec.Code)
	}

	if err := ot.store.Save(context.Background(), project, "bot@example.com", "refresh"); err != nil {
		t.Fatal(err)
	}
	rec = ot.do("GET", "/v1/projects/"+project+"/crashlytics/firebase-apps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Apps []FirebaseApp `json:"apps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := FirebaseApp{
		Name:        "projects/123456/apps/1:123456:android:abcd",
		DisplayName: "Demo Android",
		Platform:    "ANDROID",
		Namespace:   "com.example.demo",
		Project:     "Demo",
	}
	if len(body.Apps) != 1 || body.Apps[0] != want {
		t.Errorf("apps = %+v, want [%+v]", body.Apps, want)
	}
	if !appName.MatchString(want.Name) {
		t.Errorf("%s is not accepted by PUT apps", want.Name)
	}
}

func TestFirebaseAppsAPIError(t *testing.T) {
	ot := newOAuthTest(t, "refresh")
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":403,"message":"Firebase Management API has not been used"}}`, http.StatusForbidden)
	}))
	t.Cleanup(broken.Close)
	ot.handler.firebaseOpts = []option.ClientOption{option.WithEndpoint(broken.URL + "/"), option.WithHTTPClient(broken.Client())}
	if err := ot.store.Save(context.Background(), project, "bot@example.com", "refresh"); err != nil {
		t.Fatal(err)
	}

	rec := ot.do("GET", "/v1/projects/"+project+"/crashlytics/firebase-apps", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Firebase Management API") {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
}
