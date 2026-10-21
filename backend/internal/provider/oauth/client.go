package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

const (
	// DefaultMaxRateLimitWait bounds how long one call waits out a 429.
	DefaultMaxRateLimitWait = 30 * time.Second
	// defaultRateLimitWait is used when a 429 carries no usable hint.
	defaultRateLimitWait = time.Minute
	maxAttempts          = 3
	maxBody              = 4 << 20
)

// Auth supplies the bearer token for a request and renews it after a 401.
type Auth interface {
	token(ctx context.Context) (string, error)
	renew(ctx context.Context, rejected string) (string, error)
}

// For authenticates as one connection, through its stored token.
func (t *Tokens) For(connectionID uuid.UUID) Auth {
	return connAuth{t: t, id: connectionID}
}

type connAuth struct {
	t  *Tokens
	id uuid.UUID
}

func (a connAuth) token(ctx context.Context) (string, error) { return a.t.Access(ctx, a.id) }
func (a connAuth) renew(ctx context.Context, rejected string) (string, error) {
	return a.t.Renew(ctx, a.id, rejected)
}

// StaticToken authenticates with a token that is not stored yet, such as
// the one just obtained in the connect flow. It cannot be renewed.
func StaticToken(token string) Auth { return staticAuth(token) }

type staticAuth string

func (s staticAuth) token(context.Context) (string, error) { return string(s), nil }
func (s staticAuth) renew(context.Context, string) (string, error) {
	return "", errors.New("oauth: token rejected")
}

// APIError is a non-2xx answer from the tracker's API.
type APIError struct {
	Tracker string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s API: %d %s: %s", e.Tracker, e.Status, http.StatusText(e.Status), e.Message)
}

// IsStatus reports whether err is an APIError with that status.
func IsStatus(err error, status int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == status
}

// Client calls one tracker's JSON REST API.
type Client struct {
	// Tracker names the tracker in errors and logs.
	Tracker string
	// BaseURL is the API root, with a trailing slash. Relative paths are
	// resolved against it, and absolute URLs (pagination links) must share
	// its scheme and host, so a token is never sent anywhere else.
	BaseURL string
	HTTP    *http.Client
	// MaxRateLimitWait: 0 means DefaultMaxRateLimitWait; negative never
	// waits.
	MaxRateLimitWait time.Duration
}

// Do sends one request and decodes the JSON answer into out (when not nil).
// in, when not nil, is sent as JSON. It returns the response headers, for
// pagination.
//
// A 401 is retried once with a renewed token: the request was rejected,
// not processed. A 429 is waited out (Retry-After, or GitLab's
// RateLimit-Reset) when the wait fits MaxRateLimitWait and the context
// deadline, up to three attempts; otherwise Do returns
// *issue.RateLimitError. Nothing else is retried.
func (c *Client) Do(ctx context.Context, auth Auth, method, path string, in, out any) (http.Header, error) {
	target, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if in != nil {
		if payload, err = json.Marshal(in); err != nil {
			return nil, err
		}
	}

	token, err := auth.token(ctx)
	if err != nil {
		return nil, err
	}
	renewed := false
	for attempt := 1; ; attempt++ {
		status, header, body, err := c.send(ctx, method, target, payload, token)
		if err != nil {
			return nil, err
		}
		switch {
		case status == http.StatusUnauthorized && !renewed:
			renewed = true
			attempt--
			fresh, err := auth.renew(ctx, token)
			if err != nil {
				return nil, fmt.Errorf("%s: the connection's authorization was rejected: %w", c.Tracker, err)
			}
			token = fresh
			continue
		case status == http.StatusTooManyRequests:
			wait := rateLimitWait(header)
			rl := &issue.RateLimitError{Tracker: c.Tracker, Wait: wait, Err: c.apiError(status, body)}
			if attempt >= maxAttempts || wait > c.maxWait() || !fitsDeadline(ctx, wait) {
				slog.WarnContext(ctx, c.Tracker+" rate limit: giving up", "retry_after", wait, "attempt", attempt)
				return nil, rl
			}
			slog.WarnContext(ctx, c.Tracker+" rate limit: waiting before retry", "wait", wait, "attempt", attempt)
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, rl
			}
			continue
		case status < 200 || status > 299:
			return nil, c.apiError(status, body)
		}
		if out != nil && len(body) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return nil, fmt.Errorf("%s: decode %s %s: %w", c.Tracker, method, target.Path, err)
			}
		}
		return header, nil
	}
}

func (c *Client) send(ctx context.Context, method string, target *url.URL, payload []byte, token string) (int, http.Header, []byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s: %s %s: %w", c.Tracker, method, target.Path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s: read %s %s: %w", c.Tracker, method, target.Path, err)
	}
	return resp.StatusCode, resp.Header, raw, nil
}

func (c *Client) resolve(path string) (*url.URL, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%s: bad API URL: %w", c.Tracker, err)
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		if u.Scheme != base.Scheme || u.Host != base.Host {
			return nil, fmt.Errorf("%s: refusing to send the token to %s", c.Tracker, u.Host)
		}
		return u, nil
	}
	// Parse the relative path as-is so escaped slashes (%2F, GitLab project
	// paths) are kept.
	rel, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(rel), nil
}

func (c *Client) maxWait() time.Duration {
	switch {
	case c.MaxRateLimitWait == 0:
		return DefaultMaxRateLimitWait
	case c.MaxRateLimitWait < 0:
		return -1
	}
	return c.MaxRateLimitWait
}

// apiError extracts the tracker's message from an error body. It knows
// GitLab ({"message": …} or {"error": …}) and Bitbucket
// ({"error": {"message": …}}).
func (c *Client) apiError(status int, body []byte) *APIError {
	var parsed struct {
		Message json.RawMessage `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	msg := ""
	if json.Unmarshal(body, &parsed) == nil {
		msg = jsonText(parsed.Message)
		if msg == "" {
			msg = jsonText(parsed.Error)
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
	}
	return &APIError{Tracker: c.Tracker, Status: status, Message: msg}
}

// jsonText renders a message that may be a string, an object with a
// "message" field, or anything else JSON.
func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	return string(raw)
}

// rateLimitWait reads how long to wait after a 429: Retry-After (seconds
// or an HTTP date), else RateLimit-Reset (GitLab: Unix seconds), else a
// minute.
func rateLimitWait(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return atLeastSecond(time.Duration(secs) * time.Second)
		}
		if t, err := http.ParseTime(v); err == nil {
			return atLeastSecond(time.Until(t))
		}
	}
	if v := h.Get("RateLimit-Reset"); v != "" {
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
			return atLeastSecond(time.Until(time.Unix(secs, 0)))
		}
	}
	return defaultRateLimitWait
}

func atLeastSecond(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	return d
}

func fitsDeadline(ctx context.Context, wait time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > wait
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
