package crashlytics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
	firebase "google.golang.org/api/firebase/v1beta1"
	"google.golang.org/api/option"
)

// maxFirebaseProjects caps how many Firebase projects are searched for
// apps, so an account with access to many projects stays fast.
const maxFirebaseProjects = 50

// FirebaseApp is a Firebase app the connected account can read, offered to
// the owner when choosing the project's apps.
type FirebaseApp struct {
	// Name is the app as Find expects it: projects/{number}/apps/{app id}.
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	// Platform is ANDROID, IOS or WEB.
	Platform string `json:"platform"`
	// Namespace is the Android package or the iOS bundle ID.
	Namespace string `json:"namespace"`
	// Project is the Firebase project's display name.
	Project string `json:"project"`
}

// firebaseApps lists the apps of every Firebase project the connected
// account can read, through the Firebase Management API.
func (h *Handler) firebaseApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := h.store.Get(ctx, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotConnected) {
		writeError(w, http.StatusConflict, "connect Crashlytics for this project first")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	apps, err := h.listFirebaseApps(ctx, conn.RefreshToken)
	if err != nil {
		// Usually the Firebase Management API is not enabled on the quota
		// project; the owner can still type the apps by hand.
		slog.Warn("crashlytics: list Firebase apps", "err", err)
		writeError(w, http.StatusBadGateway, "cannot list the Firebase apps: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
}

func (h *Handler) listFirebaseApps(ctx context.Context, refreshToken string) ([]FirebaseApp, error) {
	if h.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, h.httpClient)
	}
	ts := h.oauth.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	opts := []option.ClientOption{option.WithTokenSource(ts)}
	if h.quotaProject != "" {
		opts = append(opts, option.WithQuotaProject(h.quotaProject))
	}
	opts = append(opts, h.firebaseOpts...)
	svc, err := firebase.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	var projects []*firebase.FirebaseProject
	err = svc.Projects.List().PageSize(maxFirebaseProjects).Pages(ctx, func(resp *firebase.ListFirebaseProjectsResponse) error {
		projects = append(projects, resp.Results...)
		if len(projects) >= maxFirebaseProjects {
			return errStopPaging
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopPaging) {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	apps := []FirebaseApp{}
	for _, p := range projects {
		number := strconv.FormatInt(p.ProjectNumber, 10)
		err := svc.Projects.SearchApps("projects/"+p.ProjectId).Pages(ctx, func(resp *firebase.SearchFirebaseAppsResponse) error {
			for _, a := range resp.Apps {
				if a.State == "DELETED" {
					continue
				}
				apps = append(apps, FirebaseApp{
					Name:        "projects/" + number + "/apps/" + a.AppId,
					DisplayName: a.DisplayName,
					Platform:    a.Platform,
					Namespace:   a.Namespace,
					Project:     p.DisplayName,
				})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("list apps of %s: %w", p.ProjectId, err)
		}
	}
	return apps, nil
}

var errStopPaging = errors.New("stop paging")
