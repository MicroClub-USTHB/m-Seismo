package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

func TestHTTPClient_ListTags(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/octo/app/tags" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`[{"name":"v1.2.0"},{"name":"v1.1.0"}]`))
		}))
		defer srv.Close()

		c := NewHTTPClient(&config.Config{GitHub: config.GitHubConfig{APIURL: srv.URL}})
		tags, err := c.ListTags(context.Background(), "github", "octo/app")
		if err != nil {
			t.Fatalf("ListTags() error = %v", err)
		}
		if !slices.Equal(tags, []string{"v1.2.0", "v1.1.0"}) {
			t.Errorf("tags = %v, want [v1.2.0 v1.1.0]", tags)
		}
	})

	t.Run("gitlab", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.EscapedPath() != "/projects/group%2Fsub%2Fapp/repository/tags" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`[{"name":"v2.0.0"},{"name":"v1.9.0"}]`))
		}))
		defer srv.Close()

		c := NewHTTPClient(&config.Config{GitLab: config.OAuthTrackerConfig{APIURL: srv.URL}})
		tags, err := c.ListTags(context.Background(), "gitlab", "group/sub/app")
		if err != nil {
			t.Fatalf("ListTags() error = %v", err)
		}
		if !slices.Equal(tags, []string{"v2.0.0", "v1.9.0"}) {
			t.Errorf("tags = %v, want [v2.0.0 v1.9.0]", tags)
		}
	})

	t.Run("bitbucket", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repositories/ws/repo/refs/tags" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"values":[{"name":"v3.0.0"},{"name":"v2.9.0"}]}`))
		}))
		defer srv.Close()

		c := NewHTTPClient(&config.Config{Bitbucket: config.OAuthTrackerConfig{APIURL: srv.URL}})
		tags, err := c.ListTags(context.Background(), "bitbucket", "ws/repo")
		if err != nil {
			t.Fatalf("ListTags() error = %v", err)
		}
		if !slices.Equal(tags, []string{"v3.0.0", "v2.9.0"}) {
			t.Errorf("tags = %v, want [v3.0.0 v2.9.0]", tags)
		}
	})

	t.Run("non-2xx returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		c := NewHTTPClient(&config.Config{GitHub: config.GitHubConfig{APIURL: srv.URL}})
		if _, err := c.ListTags(context.Background(), "github", "private/repo"); err == nil {
			t.Fatal("expected error on 401 response")
		}
	})
}
