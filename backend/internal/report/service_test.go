package report

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type fakeQuerier struct {
	db.Querier
	app db.App
	// deleted tracks the report IDs removed by DeleteReportByReportID.
	deleted map[uuid.UUID]bool
	// byInstallation counts reports per installation, consumed by
	// DeleteReportsByInstallation.
	byInstallation map[uuid.UUID]int64
}

func (f *fakeQuerier) GetApp(_ context.Context, arg db.GetAppParams) (db.App, error) {
	if arg.ID != f.app.ID || arg.OwnerID != f.app.OwnerID {
		return db.App{}, pgx.ErrNoRows
	}
	return f.app, nil
}

func (f *fakeQuerier) DeleteReportByReportID(_ context.Context, arg db.DeleteReportByReportIDParams) (int64, error) {
	if arg.AppID != f.app.ID || f.deleted == nil || !f.deleted[arg.ReportID] {
		return 0, nil
	}
	delete(f.deleted, arg.ReportID)
	return 1, nil
}

func (f *fakeQuerier) DeleteReportsByInstallation(_ context.Context, arg db.DeleteReportsByInstallationParams) (int64, error) {
	if arg.AppID != f.app.ID {
		return 0, nil
	}
	return f.byInstallation[arg.InstallationID], nil
}

func kind(err error) apperr.Kind {
	k, _ := apperr.Classify(err)
	return k
}

func TestDeleteReport(t *testing.T) {
	owner := uuid.New()
	reportID := uuid.New()
	q := &fakeQuerier{
		app:     db.App{ID: uuid.New(), OwnerID: owner},
		deleted: map[uuid.UUID]bool{reportID: true},
	}
	svc := NewService(q)
	ctx := context.Background()

	if err := svc.DeleteReport(ctx, owner, q.app.ID, reportID); err != nil {
		t.Fatalf("DeleteReport() err = %v", err)
	}

	if err := svc.DeleteReport(ctx, owner, q.app.ID, reportID); kind(err) != apperr.KindNotFound {
		t.Errorf("deleting an already-deleted report: err = %v", err)
	}

	if err := svc.DeleteReport(ctx, uuid.New(), q.app.ID, reportID); kind(err) != apperr.KindNotFound {
		t.Errorf("another owner deleted a report: err = %v", err)
	}
}

func TestDeleteByInstallation(t *testing.T) {
	owner := uuid.New()
	installationID := uuid.New()
	q := &fakeQuerier{
		app:            db.App{ID: uuid.New(), OwnerID: owner},
		byInstallation: map[uuid.UUID]int64{installationID: 3},
	}
	svc := NewService(q)
	ctx := context.Background()

	n, err := svc.DeleteByInstallation(ctx, owner, q.app.ID, installationID)
	if err != nil || n != 3 {
		t.Fatalf("DeleteByInstallation() = %d, %v", n, err)
	}

	// No reports for this installation: not an error, just zero.
	n, err = svc.DeleteByInstallation(ctx, owner, q.app.ID, uuid.New())
	if err != nil || n != 0 {
		t.Fatalf("DeleteByInstallation() = %d, %v", n, err)
	}

	if _, err := svc.DeleteByInstallation(ctx, uuid.New(), q.app.ID, installationID); kind(err) != apperr.KindNotFound {
		t.Errorf("another owner deleted by installation: err = %v", err)
	}
}
