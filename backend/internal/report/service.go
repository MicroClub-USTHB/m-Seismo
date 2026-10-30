// Package report lets an app's owner delete reports, to honour a user's
// deletion request (PRD §8). Attachments cascade; report deliveries and the
// issues they point to are left as they are.
package report

import (
	"context"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type Service struct {
	q db.Querier
}

func NewService(q db.Querier) *Service {
	return &Service{q: q}
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
