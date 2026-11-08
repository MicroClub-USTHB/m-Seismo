// Package report lets an app's owner delete reports, to honour a user's
// deletion request (PRD §8). Attachments cascade; report deliveries and the
// issues they point to are left as they are.
package report

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"mime"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type Service struct {
	q db.Querier
}

func NewService(q db.Querier) *Service {
	return &Service{q: q}
}

func (s *Service) Bundle(ctx context.Context, ownerID, appID, reportID uuid.UUID, w io.Writer) error {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return err
	}

	rep, err := s.q.GetReportByReportID(ctx, db.GetReportByReportIDParams{AppID: appID, ReportID: reportID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("report not found")
		}
		return err
	}

	attachments, err := s.q.ListReportAttachmentsWithData(ctx, rep.ID)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)

	// 1. Write report.json
	rw, err := zw.Create("report.json")
	if err != nil {
		return err
	}
	if _, err := rw.Write(rep.Payload); err != nil {
		return err
	}

	// 2. Write attachments with extension resolution and path sanitization
	for _, a := range attachments {
		name := filepath.Base(a.Name)
		if filepath.Ext(name) == "" {
			switch a.ContentType {
			case "image/png":
				name += ".png"
			case "image/jpeg":
				name += ".jpg"
			case "text/plain":
				name += ".txt"
			case "application/json":
				name += ".json"
			default:
				if exts, _ := mime.ExtensionsByType(a.ContentType); len(exts) > 0 {
					name += exts[0]
				}
			}
		}

		aw, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := aw.Write(a.Data); err != nil {
			return err
		}
	}

	return zw.Close()
}

// DeleteReport deletes one report and its attachments.
func (s *Service) DeleteReport(ctx context.Context, ownerID, appID, reportID uuid.UUID) error {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return err
	}
	n, err := s.q.DeleteReportByReportID(ctx, db.DeleteReportByReportIDParams{AppID: appID, ReportID: reportID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("report not found")
	}
	return nil
}

// DeleteByInstallation deletes every report and attachment of one
// installation of the app. It returns how many reports were deleted, zero
// when the installation sent none.
func (s *Service) DeleteByInstallation(ctx context.Context, ownerID, appID, installationID uuid.UUID) (int64, error) {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return 0, err
	}
	n, err := s.q.DeleteReportsByInstallation(ctx, db.DeleteReportsByInstallationParams{AppID: appID, InstallationID: installationID})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Service) getApp(ctx context.Context, ownerID, id uuid.UUID) (db.App, error) {
	a, err := s.q.GetApp(ctx, db.GetAppParams{ID: id, OwnerID: ownerID})
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
			return db.App{}, apperr.NotFound("app not found")
		}
		return db.App{}, err
	}
	return a, nil
}
