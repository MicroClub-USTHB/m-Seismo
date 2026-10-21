// Package oauth is the plumbing shared by trackers that reach repositories
// with per-connection OAuth 2.0 tokens (GitLab, Bitbucket):
//
//   - Endpoint: the authorization-code flow (authorize URL, code exchange,
//     refresh).
//   - Tokens: a token source per connection. It loads the encrypted token
//     from the provider.SecretStore, refreshes it shortly before it
//     expires, and saves the new one, including a rotated refresh token.
//   - Client: a small JSON REST client that authenticates with a
//     connection's token, retries once with a fresh token on 401, and
//     handles 429 like the GitHub provider does (wait if short, else
//     *issue.RateLimitError).
//
// Tokens belong to connections, and connections belong to apps, so every
// app's OAuth grant is its own.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
)

// Token is what a connection stores (JSON-encoded, then encrypted).
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
}

// Encode serialises the token for provider.SecretStore.
func (t Token) Encode() []byte {
	b, _ := json.Marshal(t)
	return b
}

// DecodeToken parses a stored token.
func DecodeToken(b []byte) (Token, error) {
	var t Token
	if err := json.Unmarshal(b, &t); err != nil || t.AccessToken == "" {
		return Token{}, errors.New("oauth: stored token is unreadable")
	}
	return t, nil
}

func (t Token) expiresWithin(d time.Duration) bool {
	return !t.Expiry.IsZero() && time.Until(t.Expiry) < d
}

// ErrRevoked means the tracker no longer accepts the connection's refresh
// token (revoked by the user, expired, or already used). Only a new connect
// flow fixes it.
var ErrRevoked = errors.New("oauth: authorization revoked or expired")

// Endpoint is one tracker's OAuth application.
type Endpoint struct {
	// Tracker names the tracker in messages ("gitlab").
	Tracker      string
	AuthURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Scopes are sent with the authorize request; empty sends none (the
	// scopes are then those configured on the OAuth consumer, as on
	// Bitbucket).
	Scopes []string
	// BasicAuth sends the client credentials with HTTP Basic (Bitbucket)
	// instead of in the form body (GitLab).
	BasicAuth bool
	HTTP      *http.Client
}

// AuthCodeURL is where the browser goes to authorise the OAuth application.
// state comes back unchanged on the callback.
func (e Endpoint) AuthCodeURL(state string) string {
	q := url.Values{
		"client_id":     {e.ClientID},
		"response_type": {"code"},
		"state":         {state},
	}
	if e.RedirectURL != "" {
		q.Set("redirect_uri", e.RedirectURL)
	}
	if len(e.Scopes) > 0 {
		q.Set("scope", strings.Join(e.Scopes, " "))
	}
	sep := "?"
	if strings.Contains(e.AuthURL, "?") {
		sep = "&"
	}
	return e.AuthURL + sep + q.Encode()
}

// Exchange trades an authorization code for a token. A code the tracker
// rejects is a client error (apperr.Invalid).
func (e Endpoint) Exchange(ctx context.Context, code string) (Token, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}}
	if e.RedirectURL != "" {
		form.Set("redirect_uri", e.RedirectURL)
	}
	tok, oauthErr, err := e.post(ctx, form)
	if err != nil {
		return Token{}, err
	}
	if oauthErr != "" {
		return Token{}, apperr.Invalid(e.Tracker + " rejected the authorization code: " + oauthErr)
	}
	return tok, nil
}

// Refresh trades a refresh token for a new token. The returned token keeps
// the old refresh token when the tracker does not send a new one.
func (e Endpoint) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	if e.RedirectURL != "" {
		form.Set("redirect_uri", e.RedirectURL)
	}
	tok, oauthErr, err := e.post(ctx, form)
	if err != nil {
		return Token{}, err
	}
	if oauthErr != "" {
		return Token{}, fmt.Errorf("%s: %w (%s); connect the app again", e.Tracker, ErrRevoked, oauthErr)
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, nil
}

// post calls the token endpoint. oauthErr is the tracker's OAuth error
// (e.g. "invalid_grant: ..."), as opposed to a transport failure.
func (e Endpoint) post(ctx context.Context, form url.Values) (tok Token, oauthErr string, err error) {
	if !e.BasicAuth {
		form.Set("client_id", e.ClientID)
		form.Set("client_secret", e.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if e.BasicAuth {
		req.SetBasicAuth(e.ClientID, e.ClientSecret)
	}

	httpc := e.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return Token{}, "", fmt.Errorf("%s: token endpoint: %w", e.Tracker, err)
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &body); err != nil {
		return Token{}, "", fmt.Errorf("%s: token endpoint answered %d with unreadable body", e.Tracker, resp.StatusCode)
	}
	if body.Error != "" || body.AccessToken == "" {
		msg := body.Error
		if body.ErrorDescription != "" {
			msg += ": " + body.ErrorDescription
		}
		if msg == "" {
			msg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		if resp.StatusCode >= 500 {
			return Token{}, "", fmt.Errorf("%s: token endpoint: %s", e.Tracker, msg)
		}
		return Token{}, msg, nil
	}
	tok = Token{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken}
	if body.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return tok, "", nil
}

// Store is where Tokens loads and saves a connection's token. It is
// provider.SecretStore; it is declared here so this package does not
// depend on provider.
type Store interface {
	Load(ctx context.Context, connectionID uuid.UUID) ([]byte, error)
	Save(ctx context.Context, connectionID uuid.UUID, secret []byte) error
}

// DefaultLeeway is how long before expiry a token is refreshed.
const DefaultLeeway = time.Minute

// Tokens hands out access tokens per connection, refreshing them when they
// are about to expire. Refreshes of one connection are serialised, which
// matters for GitLab: a refresh token is single-use, so two concurrent
// refreshes would make one of them fail. (This holds within one backend
// process; see the docs for running several.)
type Tokens struct {
	store    Store
	endpoint Endpoint
	leeway   time.Duration

	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex
}

func NewTokens(store Store, endpoint Endpoint) *Tokens {
	return &Tokens{store: store, endpoint: endpoint, leeway: DefaultLeeway, locks: map[uuid.UUID]*sync.Mutex{}}
}

func (t *Tokens) lock(id uuid.UUID) *sync.Mutex {
	t.mu.Lock()
	defer t.mu.Unlock()
	l, ok := t.locks[id]
	if !ok {
		l = &sync.Mutex{}
		t.locks[id] = l
	}
	return l
}

// Access returns a usable access token for the connection, refreshing it
// first when it expires within the leeway.
func (t *Tokens) Access(ctx context.Context, connectionID uuid.UUID) (string, error) {
	return t.get(ctx, connectionID, "")
}

// Renew is called after the tracker rejected rejected (HTTP 401). It
// refreshes the token unless another caller already replaced it.
func (t *Tokens) Renew(ctx context.Context, connectionID uuid.UUID, rejected string) (string, error) {
	return t.get(ctx, connectionID, rejected)
}

func (t *Tokens) get(ctx context.Context, id uuid.UUID, rejected string) (string, error) {
	l := t.lock(id)
	l.Lock()
	defer l.Unlock()

	raw, err := t.store.Load(ctx, id)
	if err != nil {
		return "", fmt.Errorf("%s: load token of connection %s: %w", t.endpoint.Tracker, id, err)
	}
	tok, err := DecodeToken(raw)
	if err != nil {
		return "", err
	}

	force := rejected != ""
	if force && tok.AccessToken != rejected {
		return tok.AccessToken, nil // already renewed by someone else
	}
	if !force && !tok.expiresWithin(t.leeway) {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		if force {
			return "", fmt.Errorf("%s: %w (no refresh token); connect the app again", t.endpoint.Tracker, ErrRevoked)
		}
		return tok.AccessToken, nil
	}

	fresh, err := t.endpoint.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := t.store.Save(ctx, id, fresh.Encode()); err != nil {
		// The tracker may have rotated the refresh token, so losing this
		// write can break the connection at the next refresh. The access
		// token is still good for now.
		slog.ErrorContext(ctx, "oauth token refreshed but not saved",
			"tracker", t.endpoint.Tracker, "connection", id, "err", err)
	}
	return fresh.AccessToken, nil
}
