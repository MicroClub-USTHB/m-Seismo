package crashprovider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/app"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

type verifier struct{ owner uuid.UUID }

func (v verifier) Verify(token string) (uuid.UUID, error) {
	if token != "owner-token" {
		return uuid.Nil, apperr.Unauthorized("invalid token")
	}
	return v.owner, nil
}

func TestRoutesCoexistWithAppsAndRequireOwnerAuthentication(t *testing.T) {
	s, _, owner, appID, _ := setup(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected upstream request") })
	require := httpx.NewAuthMiddleware(verifier{owner: owner})
	for _, reverse := range []bool{false, true} {
		router := chi.NewRouter()
		apps := app.NewHandler(nil, nil, require)
		crashes := NewHandler(s, require)
		// fx value groups do not guarantee registration order.
		router.Route("/api/v1", func(r chi.Router) {
			if reverse {
				crashes.Mount(r)
				apps.Mount(r)
			} else {
				apps.Mount(r)
				crashes.Mount(r)
			}
		})
		for _, token := range []string{"", "msk_sdk-key"} {
			req := httptest.NewRequest("GET", "/api/v1/apps/"+appID.String()+"/crash-provider", nil)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != 401 {
				t.Fatalf("unauthorized route returned %d: %s", response.Code, response.Body.String())
			}
		}
		body := `{"provider":"crashlytics","crashlytics":{"project_id":"firebase","app_id":"1:123:web:456"}}`
		req := httptest.NewRequest("PUT", "/api/v1/apps/"+appID.String()+"/crash-provider", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer owner-token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 200 {
			t.Fatalf("configure returned %d: %s", response.Code, response.Body.String())
		}
		req = httptest.NewRequest("GET", "/api/v1/apps/"+appID.String()+"/crash-provider", nil)
		req.Header.Set("Authorization", "Bearer owner-token")
		response = httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"provider":"crashlytics"`) {
			t.Fatalf("get returned %d: %s", response.Code, response.Body.String())
		}
	}
}
