package report

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
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
	reports        map[uuid.UUID]db.Report
	attachments    map[uuid.UUID][]db.ListReportAttachmentsWithDataRow
}

func (f *fakeQuerier) GetApp(_ context.Context, arg db.GetAppParams) (db.App, error) {
	if arg.ID != f.app.ID || arg.OwnerID != f.app.OwnerID {
		return db.App{}, pgx.ErrNoRows
	}
	return f.app, nil
}

func (f *fakeQuerier) GetReportByReportID(_ context.Context, arg db.GetReportByReportIDParams) (db.Report, error) {
	if arg.AppID != f.app.ID {
		return db.Report{}, pgx.ErrNoRows
	}
	r, ok := f.reports[arg.ReportID]
	if !ok || r.AppID != arg.AppID {
		return db.Report{}, pgx.ErrNoRows
	}
	return r, nil
}

func (f *fakeQuerier) ListReportAttachmentsWithData(_ context.Context, reportID uuid.UUID) ([]db.ListReportAttachmentsWithDataRow, error) {
	if f.attachments == nil {
		return nil, nil
	}
	return f.attachments[reportID], nil
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

func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader() failed: %v", err)
	}
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open file %q in zip: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("failed to read file %q in zip: %v", f.Name, err)
		}
		files[f.Name] = content
	}
	return files
}

func TestExportBundle(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	appID := uuid.New()
	reportID := uuid.New()
	internalID := uuid.New()

	reportPayload := []byte(`{"id":"` + reportID.String() + `","device":{"model":"Pixel 8"},"trigger":"shake"}`)
	screenshotData := []byte("\x89PNG\r\n\x1a\nfake-screenshot-binary")
	logsData := []byte("2026-11-08 20:00:00 [INFO] app started\n2026-11-08 20:00:05 [ERROR] crash")

	t.Run("happy path with attachments", func(t *testing.T) {
		q := &fakeQuerier{
			app: db.App{ID: appID, OwnerID: owner},
			reports: map[uuid.UUID]db.Report{
				reportID: {ID: internalID, AppID: appID, ReportID: reportID, Payload: reportPayload},
			},
			attachments: map[uuid.UUID][]db.ListReportAttachmentsWithDataRow{
				internalID: {
					{Name: "screenshot.png", ContentType: "image/png", Data: screenshotData},
					{Name: "server-logs.txt", ContentType: "text/plain", Data: logsData},
				},
			},
		}
		svc := NewService(q)

		var buf bytes.Buffer
		if err := svc.Bundle(ctx, owner, appID, reportID, &buf); err != nil {
			t.Fatalf("Bundle() unexpected err: %v", err)
		}

		files := unzip(t, buf.Bytes())
		if len(files) != 3 {
			t.Fatalf("expected 3 files in zip, got %d", len(files))
		}

		if got, ok := files["report.json"]; !ok {
			t.Errorf("missing report.json in zip")
		} else if !bytes.Equal(got, reportPayload) {
			t.Errorf("report.json content mismatch: got %s, want %s", got, reportPayload)
		}

		if got, ok := files["screenshot.png"]; !ok {
			t.Errorf("missing screenshot.png in zip")
		} else if !bytes.Equal(got, screenshotData) {
			t.Errorf("screenshot.png content mismatch: got %v, want %v", got, screenshotData)
		}

		if got, ok := files["server-logs.txt"]; !ok {
			t.Errorf("missing server-logs.txt in zip")
		} else if !bytes.Equal(got, logsData) {
			t.Errorf("server-logs.txt content mismatch: got %s, want %s", got, logsData)
		}
	})

	t.Run("report without attachments", func(t *testing.T) {
		q := &fakeQuerier{
			app: db.App{ID: appID, OwnerID: owner},
			reports: map[uuid.UUID]db.Report{
				reportID: {ID: internalID, AppID: appID, ReportID: reportID, Payload: reportPayload},
			},
			attachments: map[uuid.UUID][]db.ListReportAttachmentsWithDataRow{
				internalID: {},
			},
		}
		svc := NewService(q)

		var buf bytes.Buffer
		if err := svc.Bundle(ctx, owner, appID, reportID, &buf); err != nil {
			t.Fatalf("Bundle() unexpected err: %v", err)
		}

		files := unzip(t, buf.Bytes())
		if len(files) != 1 {
			t.Fatalf("expected 1 file in zip, got %d", len(files))
		}
		if got, ok := files["report.json"]; !ok {
			t.Errorf("missing report.json in zip")
		} else if !bytes.Equal(got, reportPayload) {
			t.Errorf("report.json content mismatch: got %s, want %s", got, reportPayload)
		}
	})

	t.Run("attachment filename extension resolution and sanitization", func(t *testing.T) {
		q := &fakeQuerier{
			app: db.App{ID: appID, OwnerID: owner},
			reports: map[uuid.UUID]db.Report{
				reportID: {ID: internalID, AppID: appID, ReportID: reportID, Payload: reportPayload},
			},
			attachments: map[uuid.UUID][]db.ListReportAttachmentsWithDataRow{
				internalID: {
					// Ingestion saves screenshot as "screenshot" without extension:
					{Name: "screenshot", ContentType: "image/png", Data: screenshotData},
					// Path traversal attempt should be stripped to base name:
					{Name: "../../nested/server-logs.txt", ContentType: "text/plain", Data: logsData},
				},
			},
		}
		svc := NewService(q)

		var buf bytes.Buffer
		if err := svc.Bundle(ctx, owner, appID, reportID, &buf); err != nil {
			t.Fatalf("Bundle() unexpected err: %v", err)
		}

		files := unzip(t, buf.Bytes())
		if _, ok := files["screenshot.png"]; !ok {
			t.Errorf("expected screenshot.png after extension resolution, got files: %v", files)
		}
		if _, ok := files["server-logs.txt"]; !ok {
			t.Errorf("expected server-logs.txt after path sanitization, got files: %v", files)
		}
	})

	t.Run("missing report", func(t *testing.T) {
		nonExistentReportID := uuid.New()
		q := &fakeQuerier{
			app:     db.App{ID: appID, OwnerID: owner},
			reports: map[uuid.UUID]db.Report{},
		}
		svc := NewService(q)

		var buf bytes.Buffer
		err := svc.Bundle(ctx, owner, appID, nonExistentReportID, &buf)
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound for non-existent report, got %v (kind %v)", err, kind(err))
		}
		if buf.Len() != 0 {
			t.Errorf("expected no bytes written to writer on error, got %d bytes", buf.Len())
		}
	})

	t.Run("wrong owner", func(t *testing.T) {
		strangerOwner := uuid.New()
		q := &fakeQuerier{
			app: db.App{ID: appID, OwnerID: owner},
			reports: map[uuid.UUID]db.Report{
				reportID: {ID: internalID, AppID: appID, ReportID: reportID, Payload: reportPayload},
			},
		}
		svc := NewService(q)

		var buf bytes.Buffer
		err := svc.Bundle(ctx, strangerOwner, appID, reportID, &buf)
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound for unauthorized owner, got %v (kind %v)", err, kind(err))
		}
		if buf.Len() != 0 {
			t.Errorf("expected no bytes written to writer on error, got %d bytes", buf.Len())
		}
	})

	t.Run("cross-app isolation", func(t *testing.T) {
		otherAppID := uuid.New()
		q := &fakeQuerier{
			app: db.App{ID: otherAppID, OwnerID: owner},
			reports: map[uuid.UUID]db.Report{
				// Report is under appID, but request is querying otherAppID
				reportID: {ID: internalID, AppID: appID, ReportID: reportID, Payload: reportPayload},
			},
		}
		svc := NewService(q)

		var buf bytes.Buffer
		err := svc.Bundle(ctx, owner, otherAppID, reportID, &buf)
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound when querying report under another app, got %v (kind %v)", err, kind(err))
		}
	})
}
