package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// Target is where an issue from the app goes: a linked project's default
// repository and the app connection that reaches it. Error is set, and
// ConnectionID is not, when the app has no connection for the repository.
type Target struct {
	ProjectID           uuid.UUID  `json:"project_id"`
	ProjectRepositoryID uuid.UUID  `json:"project_repository_id"`
	Repository          string     `json:"repository"`
	Tracker             db.Tracker `json:"tracker"`
	ConnectionID        *uuid.UUID `json:"connection_id"`
	AccountLogin        string     `json:"account_login,omitempty"`
	Error               string     `json:"error,omitempty"`
}

// Targets previews where the app's issues would be opened, without opening
// any: one entry per linked project that has a default repository.
func (s *Service) Targets(ctx context.Context, ownerID, appID uuid.UUID) ([]Target, error) {
	if _, err := s.get(ctx, ownerID, appID); err != nil {
		return nil, err
	}
	ts, err := s.issues.Targets(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := make([]Target, len(ts))
	for i, t := range ts {
		out[i] = Target{
			ProjectID:           t.ProjectID,
			ProjectRepositoryID: t.ProjectRepositoryID,
			Repository:          t.Repository,
			Tracker:             t.Tracker,
		}
		if t.Connection != nil {
			id := t.Connection.ID
			out[i].ConnectionID = &id
			out[i].AccountLogin = t.Connection.AccountLogin
		}
		if t.Err != nil {
			msg := t.Err.Error()
			if errors.Is(t.Err, issue.ErrNoConnection) {
				msg = "no " + string(t.Tracker) + " connection on this app covers " + t.Repository
			}
			out[i].Error = msg
		}
	}
	return out, nil
}

// The per-app connection routes. The connect flow's callback stays at
// /providers/{tracker}/callback (trackers allow one callback URL per
// m-Seismo deployment); the app is carried in the one-time state.

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	tracker := db.Tracker(chi.URLParam(r, "tracker"))
	u, err := h.conns.StartConnect(r.Context(), httpx.OwnerID(r.Context()), id, tracker)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// JSON rather than a redirect: an SPA calling this with a bearer token
	// cannot follow a redirect itself, so it navigates to url.
	httpx.JSON(w, http.StatusOK, map[string]string{"url": u})
}

func (h *Handler) listConnections(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	conns, err := h.conns.ListConnections(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, conns)
}

type attachInput struct {
	// ConnectionID is a connection of another of the owner's apps.
	ConnectionID uuid.UUID `json:"connection_id"`
}

func (h *Handler) attachConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	var in attachInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	c, err := h.conns.AttachConnection(r.Context(), httpx.OwnerID(r.Context()), id, in.ConnectionID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, c)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	connectionID, ok := uuidParam(w, r, "connectionID")
	if !ok {
		return
	}
	if err := h.conns.Disconnect(r.Context(), httpx.OwnerID(r.Context()), id, connectionID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) listConnectionRepositories(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	connectionID, ok := uuidParam(w, r, "connectionID")
	if !ok {
		return
	}
	repos, err := h.conns.ListRepositories(r.Context(), httpx.OwnerID(r.Context()), id, connectionID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, repos)
}

func (h *Handler) targets(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	ts, err := h.svc.Targets(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ts)
}
