package crashlytics

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// stateTTL is how long the owner has to finish the Google consent screen.
const stateTTL = 10 * time.Minute

// userinfoURL returns the email of the Google account behind a token.
const userinfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

// appName matches a Firebase app resource name.
var appName = regexp.MustCompile(`^projects/[^/]+/apps/[^/]+$`)

// States keeps the OAuth state value between the connect and callback
// requests, which ties the callback to the project and protects it from
// CSRF.
type States interface {
	Put(ctx context.Context, state, projectID string, ttl time.Duration) error
	// Take returns the project ID stored for state and forgets it. ok is
	// false when the state is unknown or expired.
	Take(ctx context.Context, state string) (projectID string, ok bool, err error)
}

// RedisStates is the States backed by Redis.
type RedisStates struct {
	rdb *redis.Client
}

var _ States = (*RedisStates)(nil)

func NewRedisStates(rdb *redis.Client) *RedisStates {
	return &RedisStates{rdb: rdb}
}

func stateKey(state string) string { return "seismo:crashlytics:oauth-state:" + state }

func (s *RedisStates) Put(ctx context.Context, state, projectID string, ttl time.Duration) error {
	if err := s.rdb.Set(ctx, stateKey(state), projectID, ttl).Err(); err != nil {
		return fmt.Errorf("crashlytics: store oauth state: %w", err)
	}
	return nil
}

func (s *RedisStates) Take(ctx context.Context, state string) (string, bool, error) {
	projectID, err := s.rdb.GetDel(ctx, stateKey(state)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("crashlytics: read oauth state: %w", err)
	}
	return projectID, true, nil
}

// Handler serves the HTTP endpoints that connect a project to Crashlytics,
// under /api/v1:
//
//	POST /projects/{id}/crashlytics/connect  owner: returns the Google consent URL
//	GET  /oauth/crashlytics/callback         Google redirects here after consent
//	GET  /projects/{id}/crashlytics          owner: connection status
//	PUT  /projects/{id}/crashlytics/apps     owner: sets the project's Firebase apps
type Handler struct {
	oauth       *oauth2.Config
	store       Store
	states      States
	require     httpx.AuthMiddleware
	userinfoURL string
	// httpClient, when set, is used for the calls to Google (tests).
	httpClient *http.Client
}

func NewHandler(cfg Config, store Store, states States, require httpx.AuthMiddleware) *Handler {
	return &Handler{oauth: cfg.oauth(), store: store, states: states, require: require, userinfoURL: userinfoURL}
}

// RedirectURL is where Google sends the browser back after consent.
func (h *Handler) RedirectURL() string { return h.oauth.RedirectURL }

// Mount adds the endpoints to the /api/v1 router. The project endpoints
// need the owner's bearer token and only reach the owner's own projects.
// The callback stays public because Google's redirect carries no
// credentials; it is protected by the one-time state value instead.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/oauth/crashlytics/callback", h.callback)

	owned := r.With(h.require, h.ownProject)
	owned.Post("/projects/{id}/crashlytics/connect", h.connect)
	owned.Get("/projects/{id}/crashlytics", h.status)
	owned.Put("/projects/{id}/crashlytics/apps", h.setApps)
}

// ownProject answers 404 unless the project in the path belongs to the
// authenticated owner, the same as for a project that does not exist.
func (h *Handler) ownProject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := h.store.OwnsProject(r.Context(), httpx.OwnerID(r.Context()), chi.URLParam(r, "id"))
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		internalError(w, err)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	if err := h.states.Put(r.Context(), state, projectID, stateTTL); err != nil {
		internalError(w, err)
		return
	}

	// Offline access with a forced consent screen, so Google always
	// returns a refresh token, even for an account that consented before.
	url := h.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		http.Error(w, "Google did not grant access: "+e, http.StatusBadRequest)
		return
	}

	projectID, ok, err := h.states.Take(r.Context(), q.Get("state"))
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		http.Error(w, "This link has expired or was already used. Start the connection again.", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if h.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, h.httpClient)
	}

	tok, err := h.oauth.Exchange(ctx, q.Get("code"))
	if err != nil {
		log.Printf("crashlytics: oauth exchange for project %s: %v", projectID, err)
		http.Error(w, "Could not get access from Google. Start the connection again.", http.StatusBadGateway)
		return
	}
	if tok.RefreshToken == "" {
		http.Error(w, "Google did not return a refresh token. Start the connection again.", http.StatusBadGateway)
		return
	}

	email, err := h.email(ctx, tok)
	if err != nil {
		log.Printf("crashlytics: read google account of project %s: %v", projectID, err)
		http.Error(w, "Could not read the Google account. Start the connection again.", http.StatusBadGateway)
		return
	}

	if err := h.store.Save(ctx, projectID, email, tok.RefreshToken); err != nil {
		internalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "Crashlytics is connected with %s. If not done yet, set the project's Firebase apps. You can close this page.\n", email)
}

// email returns the address of the Google account that granted tok.
func (h *Handler) email(ctx context.Context, tok *oauth2.Token) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.userinfoURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := h.oauth.Client(ctx, tok).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo: %s", resp.Status)
	}

	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("userinfo: %w", err)
	}
	if info.Email == "" {
		return "", errors.New("userinfo: no email")
	}
	return info.Email, nil
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	conn, err := h.store.Get(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotConnected) {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	apps := conn.Apps
	if apps == nil {
		apps = []string{}
	}
	// Never return the refresh token.
	writeJSON(w, http.StatusOK, map[string]any{
		"connected":    true,
		"google_email": conn.Email,
		"apps":         apps,
	})
}

func (h *Handler) setApps(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Apps []string `json:"apps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	for _, app := range body.Apps {
		if !appName.MatchString(app) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("app %q must look like projects/<project number>/apps/<app id>", app))
			return
		}
	}
	if body.Apps == nil {
		body.Apps = []string{}
	}

	err := h.store.SetApps(r.Context(), chi.URLParam(r, "id"), body.Apps)
	if errors.Is(err, ErrNotConnected) {
		writeError(w, http.StatusConflict, "connect Crashlytics for this project first")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	// Keep "&" readable in the consent URL, which owners may copy by hand.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func internalError(w http.ResponseWriter, err error) {
	log.Printf("crashlytics: %v", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
