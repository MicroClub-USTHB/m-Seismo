package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// ScreenshotName is the attachment name of a report's screenshot.
const ScreenshotName = "screenshot"

// Attachment is a file sent with a report.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Saved is the stored report a Save call ended up with.
type Saved struct {
	// ID is the backend's own ID for the report.
	ID uuid.UUID
	// Duplicate is true when the app had already sent this report ID; the
	// earlier copy is kept and nothing new is written.
	Duplicate bool
	// Queued is true when the pipeline has already been told about it.
	Queued bool
}

// Store keeps accepted reports.
type Store interface {
	// Save stores the report and its attachments in one transaction, once
	// per app and report ID.
	Save(ctx context.Context, appID uuid.UUID, r *Report, payload []byte, files []Attachment) (Saved, error)
	// MarkQueued records that the pipeline was told about the report.
	MarkQueued(ctx context.Context, id uuid.UUID) error
}

// PGStore is the Store backed by Postgres.
type PGStore struct {
	pool *pgxpool.Pool
}

var _ Store = (*PGStore)(nil)

func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) Save(ctx context.Context, appID uuid.UUID, r *Report, payload []byte, files []Attachment) (Saved, error) {
	reportID, err := uuid.Parse(r.ID)
	if err != nil {
		return Saved{}, fmt.Errorf("ingest: report id: %w", err)
	}
	installationID, err := uuid.Parse(r.InstallationID)
	if err != nil {
		return Saved{}, fmt.Errorf("ingest: installation id: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Saved{}, fmt.Errorf("ingest: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	row, err := q.InsertReport(ctx, db.InsertReportParams{
		AppID:          appID,
		ReportID:       reportID,
		InstallationID: installationID,
		SessionID:      r.SessionID,
		Trigger:        r.Trigger,
		OccurredAt:     r.OccurredTime(),
		Payload:        payload,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Already stored: answer from the first copy.
		prev, err := q.GetReportByReportID(ctx, db.GetReportByReportIDParams{AppID: appID, ReportID: reportID})
		if err != nil {
			return Saved{}, fmt.Errorf("ingest: load earlier copy: %w", err)
		}
		return Saved{ID: prev.ID, Duplicate: true, Queued: prev.QueuedAt != nil}, nil
	}
	if err != nil {
		return Saved{}, fmt.Errorf("ingest: insert report: %w", err)
	}

	for _, f := range files {
		err := q.InsertReportAttachment(ctx, db.InsertReportAttachmentParams{
			ReportID:    row.ID,
			Name:        f.Name,
			ContentType: f.ContentType,
			Data:        f.Data,
		})
		if err != nil {
			return Saved{}, fmt.Errorf("ingest: insert %s: %w", f.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Saved{}, fmt.Errorf("ingest: commit: %w", err)
	}
	return Saved{ID: row.ID}, nil
}

func (s *PGStore) MarkQueued(ctx context.Context, id uuid.UUID) error {
	if err := db.New(s.pool).MarkReportQueued(ctx, id); err != nil {
		return fmt.Errorf("ingest: mark queued: %w", err)
	}
	return nil
}
