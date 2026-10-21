package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
)

// Size limits from spec/ingestion.md.
const (
	maxRequestBytes    = 15 << 20
	maxReportBytes     = 1 << 20
	maxScreenshotBytes = 8 << 20
)

// retryAfterUnavailable is the Retry-After sent with a 503.
const retryAfterUnavailable = 5

// Authenticator resolves an SDK key to its app. Implemented by
// app.Service.
type Authenticator interface {
	Authenticate(ctx context.Context, rawKey string) (uuid.UUID, error)
}

// Limiter checks a rate limit. Implemented by ratelimit.Limiter.
type Limiter interface {
	Allow(ctx context.Context, key string, lim ratelimit.Limit) (ratelimit.Result, error)
}

// Limits are the rate limits applied to reports.
type Limits struct {
	// PerApp applies to every report sent with the app's keys.
	PerApp ratelimit.Limit
	// PerInstallation applies to the reports of one installation of the app.
	PerInstallation ratelimit.Limit
}

// Handler serves POST /api/v1/reports, as described in spec/ingestion.md.
type Handler struct {
	auth    Authenticator
	limiter Limiter
	limits  Limits
	store   Store
	queue   queue.Publisher
}

func NewHandler(auth Authenticator, limiter Limiter, limits Limits, store Store, pub queue.Publisher) *Handler {
	return &Handler{auth: auth, limiter: limiter, limits: limits, store: store, queue: pub}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/reports", h.submit)
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Key.
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || key == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid app key")
		return
	}
	appID, err := h.auth.Authenticate(ctx, key)
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindUnauthorized {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid app key")
			return
		}
		h.unavailable(w, r, "authenticate", err)
		return
	}

	// 2. Rate limit for the app, before reading the body.
	if !h.allow(w, r, "app:"+appID.String(), h.limits.PerApp, "too many reports from this app") {
		return
	}

	// 3. Read the parts, within the size limits.
	reportPart, files, status, code, msg := readParts(w, r)
	if status != 0 {
		writeError(w, status, code, msg)
		return
	}

	// 4. Decode and validate.
	report, err := ParseReport(reportPart)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_report", err.Error())
		return
	}
	if err := report.Validate(); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			writeError(w, http.StatusBadRequest, "unsupported_version",
				fmt.Sprintf("schema_version %d is not supported; this server accepts %d", report.SchemaVersion, SchemaVersion))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_report", err.Error())
		return
	}

	report.Normalize()

	// 5. Rate limit for the installation, now that it is known.
	installKey := "installation:" + appID.String() + ":" + report.InstallationID
	if !h.allow(w, r, installKey, h.limits.PerInstallation, "too many reports from this installation") {
		return
	}

	// 6. Store once per report ID.
	payload, err := json.Marshal(report)
	if err != nil {
		h.unavailable(w, r, "encode report", err)
		return
	}
	saved, err := h.store.Save(ctx, appID, report, payload, files)
	if err != nil {
		h.unavailable(w, r, "store report", err)
		return
	}

	// 7. Tell the pipeline, unless an earlier attempt already did. If this
	// fails the report stays stored; the SDK's retry publishes it again.
	if !saved.Queued {
		if err := h.queue.ReportAccepted(ctx, queue.ReportAccepted{ID: saved.ID, AppID: appID}); err != nil {
			h.unavailable(w, r, "queue report", err)
			return
		}
		if err := h.store.MarkQueued(ctx, saved.ID); err != nil {
			// The message is out; at worst a retry publishes it again and
			// JetStream drops the duplicate.
			slog.WarnContext(ctx, "ingest: report queued but not marked", "report", saved.ID, "err", err)
		}
	}

	// 8. Acknowledge.
	writeJSON(w, http.StatusAccepted, map[string]string{"id": report.ID, "status": "accepted"})
}

// allow applies a rate limit and writes the 429 when it is exceeded. When
// Redis can't be reached the report is let through: losing abuse
// protection for a while is better than refusing every report.
func (h *Handler) allow(w http.ResponseWriter, r *http.Request, key string, lim ratelimit.Limit, msg string) bool {
	res, err := h.limiter.Allow(r.Context(), key, lim)
	if err != nil {
		slog.WarnContext(r.Context(), "ingest: rate limit unavailable, letting the report through", "key", key, "err", err)
		return true
	}
	if res.Allowed {
		return true
	}
	seconds := int(math.Ceil(res.RetryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, "rate_limited", msg)
	return false
}

func (h *Handler) unavailable(w http.ResponseWriter, r *http.Request, step string, err error) {
	slog.ErrorContext(r.Context(), "ingest: "+step+" failed", "err", err)
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterUnavailable))
	writeError(w, http.StatusServiceUnavailable, "unavailable", "the server can't take reports right now, try again later")
}

// readParts reads the multipart body. On failure it returns the status,
// code and message to answer with.
func readParts(w http.ResponseWriter, r *http.Request) (report []byte, files []Attachment, status int, code, msg string) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, nil, http.StatusBadRequest, "invalid_request", "the body must be multipart/form-data"
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, nil, http.StatusBadRequest, "invalid_request", "the body must be multipart/form-data"
	}

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return failedRead(err)
		}

		switch part.FormName() {
		case "report":
			data, tooBig, err := readLimited(part, maxReportBytes)
			if err != nil {
				return failedRead(err)
			}
			if tooBig {
				return nil, nil, http.StatusRequestEntityTooLarge, "too_large", "the report part is larger than 1 MB"
			}
			report = data

		case "screenshot":
			data, tooBig, err := readLimited(part, maxScreenshotBytes)
			if err != nil {
				return failedRead(err)
			}
			if tooBig {
				return nil, nil, http.StatusRequestEntityTooLarge, "too_large", "the screenshot is larger than 8 MB"
			}
			// Trust the bytes, not the declared content type.
			ct := http.DetectContentType(data)
			if ct != "image/jpeg" && ct != "image/png" {
				return nil, nil, http.StatusUnsupportedMediaType, "unsupported_media_type", "the screenshot must be a JPEG or PNG image"
			}
			files = []Attachment{{Name: "screenshot", ContentType: ct, Data: data}}

		default:
			// Unknown parts are ignored.
			if _, err := io.Copy(io.Discard, part); err != nil {
				return failedRead(err)
			}
		}
	}

	if report == nil {
		return nil, nil, http.StatusBadRequest, "invalid_request", "the report part is missing"
	}
	return report, files, 0, "", ""
}

// readLimited reads at most limit bytes from rd; tooBig is true when there
// was more.
func readLimited(rd io.Reader, limit int64) (data []byte, tooBig bool, err error) {
	data, err = io.ReadAll(io.LimitReader(rd, limit+1))
	if err != nil {
		return nil, false, err
	}
	return data, int64(len(data)) > limit, nil
}

func failedRead(err error) ([]byte, []Attachment, int, string, string) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return nil, nil, http.StatusRequestEntityTooLarge, "too_large", "the request is larger than 15 MB"
	}
	return nil, nil, http.StatusBadRequest, "invalid_request", "the multipart body could not be read"
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Code: code, Message: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
