package report

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
)

type fakePublisher struct {
	published []queue.ReportAccepted
}

func (f *fakePublisher) ReportAccepted(_ context.Context, msg queue.ReportAccepted) error {
	f.published = append(f.published, msg)
	return nil
}

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
	deliveries     map[uuid.UUID][]db.ReportDelivery
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

func (f *fakeQuerier) ListReportDeliveries(_ context.Context, reportID uuid.UUID) ([]db.ReportDelivery, error) {
	if f.deliveries == nil {
		return nil, nil
	}
	return f.deliveries[reportID], nil
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
	svc := NewService(q, nil)
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
		svc := NewService(q, nil)

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
		svc := NewService(q, nil)

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
		svc := NewService(q, nil)

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
		svc := NewService(q, nil)

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
		svc := NewService(q, nil)

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
		svc := NewService(q, nil)

		var buf bytes.Buffer
		err := svc.Bundle(ctx, owner, otherAppID, reportID, &buf)
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound when querying report under another app, got %v (kind %v)", err, kind(err))
		}
	})
}

func TestListReports(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	appID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	repID1 := uuid.New()
	repID2 := uuid.New()
	repID3 := uuid.New()
	repID4 := uuid.New()
	repID5 := uuid.New()

	intID1 := uuid.New()
	intID2 := uuid.New()
	intID3 := uuid.New()
	intID4 := uuid.New()
	intID5 := uuid.New()

	proc1 := now.Add(-30 * time.Second)
	proc3 := now.Add(-90 * time.Second)

	// 5 reports sorted by OccurredAt descending:
	// 1: -1m, delivered
	// 2: -2m, failed
	// 3: -3m, delivered
	// 4: -4m, failed
	// 5: -5m, failed
	reports := map[uuid.UUID]db.Report{
		repID1: {ID: intID1, AppID: appID, ReportID: repID1, OccurredAt: now.Add(-1 * time.Minute), ProcessedAt: &proc1, Trigger: "shake"},
		repID2: {ID: intID2, AppID: appID, ReportID: repID2, OccurredAt: now.Add(-2 * time.Minute), ProcessedAt: nil, Trigger: "manual"},
		repID3: {ID: intID3, AppID: appID, ReportID: repID3, OccurredAt: now.Add(-3 * time.Minute), ProcessedAt: &proc3, Trigger: "crash"},
		repID4: {ID: intID4, AppID: appID, ReportID: repID4, OccurredAt: now.Add(-4 * time.Minute), ProcessedAt: nil, Trigger: "shake"},
		repID5: {ID: intID5, AppID: appID, ReportID: repID5, OccurredAt: now.Add(-5 * time.Minute), ProcessedAt: nil, Trigger: "manual"},
	}

	deliveries := map[uuid.UUID][]db.ReportDelivery{
		intID1: {
			{ReportID: intID1, URL: "https://github.com/org/repo/issues/10"},
		},
		intID3: {
			{ReportID: intID3, URL: "https://github.com/org/repo/issues/12"},
		},
	}

	q := &fakeQuerier{
		app:        db.App{ID: appID, OwnerID: owner},
		reports:    reports,
		deliveries: deliveries,
	}
	svc := NewService(q, nil)

	t.Run("happy path", func(t *testing.T) {
		res, err := svc.ListReports(ctx, owner, appID, ListReportsParams{Limit: 10})
		if err != nil {
			t.Fatalf("ListReports() unexpected err: %v", err)
		}
		if len(res.Reports) != 5 {
			t.Fatalf("expected 5 reports, got %d", len(res.Reports))
		}
		if res.Reports[0].Status != "delivered" {
			t.Errorf("report 0 status = %q, want 'delivered'", res.Reports[0].Status)
		}
		if len(res.Reports[0].IssueURLs) != 1 || res.Reports[0].IssueURLs[0] != "https://github.com/org/repo/issues/10" {
			t.Errorf("report 0 IssueURLs = %v, want ['https://github.com/org/repo/issues/10']", res.Reports[0].IssueURLs)
		}
		if res.Reports[1].Status != "failed" {
			t.Errorf("report 1 status = %q, want 'failed'", res.Reports[1].Status)
		}
		if len(res.Reports[1].IssueURLs) != 0 {
			t.Errorf("report 1 IssueURLs = %v, want empty", res.Reports[1].IssueURLs)
		}
		if res.Reports[2].Status != "delivered" {
			t.Errorf("report 2 status = %q, want 'delivered'", res.Reports[2].Status)
		}
		if res.Reports[3].Status != "failed" || res.Reports[4].Status != "failed" {
			t.Errorf("reports 3 and 4 should be 'failed'")
		}
	})

	t.Run("filter by status delivered", func(t *testing.T) {
		res, err := svc.ListReports(ctx, owner, appID, ListReportsParams{Status: "delivered", Limit: 10})
		if err != nil {
			t.Fatalf("ListReports() unexpected err: %v", err)
		}
		if len(res.Reports) != 2 {
			t.Fatalf("expected 2 delivered reports, got %d", len(res.Reports))
		}
		for i, r := range res.Reports {
			if r.Status != "delivered" {
				t.Errorf("report %d status = %q, want 'delivered'", i, r.Status)
			}
		}
	})

	t.Run("filter by status failed", func(t *testing.T) {
		res, err := svc.ListReports(ctx, owner, appID, ListReportsParams{Status: "failed", Limit: 10})
		if err != nil {
			t.Fatalf("ListReports() unexpected err: %v", err)
		}
		if len(res.Reports) != 3 {
			t.Fatalf("expected 3 failed reports, got %d", len(res.Reports))
		}
		for i, r := range res.Reports {
			if r.Status != "failed" {
				t.Errorf("report %d status = %q, want 'failed'", i, r.Status)
			}
		}
	})

	t.Run("pagination", func(t *testing.T) {
		page1, err := svc.ListReports(ctx, owner, appID, ListReportsParams{Limit: 2})
		if err != nil {
			t.Fatalf("page 1 unexpected err: %v", err)
		}
		if len(page1.Reports) != 2 {
			t.Fatalf("page 1 expected 2 reports, got %d", len(page1.Reports))
		}
		if page1.NextCursor == nil {
			t.Fatalf("page 1 expected next cursor, got nil")
		}
		if page1.Reports[0].ReportID != repID1 || page1.Reports[1].ReportID != repID2 {
			t.Errorf("page 1 returned unexpected reports: %v, %v", page1.Reports[0].ReportID, page1.Reports[1].ReportID)
		}

		page2, err := svc.ListReports(ctx, owner, appID, ListReportsParams{Limit: 2, Cursor: page1.NextCursor})
		if err != nil {
			t.Fatalf("page 2 unexpected err: %v", err)
		}
		if len(page2.Reports) != 2 {
			t.Fatalf("page 2 expected 2 reports, got %d", len(page2.Reports))
		}
		if page2.NextCursor == nil {
			t.Fatalf("page 2 expected next cursor, got nil")
		}
		if page2.Reports[0].ReportID != repID3 || page2.Reports[1].ReportID != repID4 {
			t.Errorf("page 2 returned unexpected reports: %v, %v", page2.Reports[0].ReportID, page2.Reports[1].ReportID)
		}

		page3, err := svc.ListReports(ctx, owner, appID, ListReportsParams{Limit: 2, Cursor: page2.NextCursor})
		if err != nil {
			t.Fatalf("page 3 unexpected err: %v", err)
		}
		if len(page3.Reports) != 1 {
			t.Fatalf("page 3 expected 1 report, got %d", len(page3.Reports))
		}
		if page3.Reports[0].ReportID != repID5 {
			t.Errorf("page 3 returned unexpected report: %v", page3.Reports[0].ReportID)
		}
		if page3.NextCursor != nil {
			t.Errorf("page 3 expected nil next cursor for last page, got %v", page3.NextCursor)
		}
	})

	t.Run("unauthorized owner / wrong app", func(t *testing.T) {
		stranger := uuid.New()
		_, err := svc.ListReports(ctx, stranger, appID, ListReportsParams{Limit: 10})
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound for unauthorized owner, got %v (kind %v)", err, kind(err))
		}
	})
}

func TestGetReport(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	appID := uuid.New()
	reportID := uuid.New()
	internalID := uuid.New()
	now := time.Now().UTC()
	payload := []byte(`{"device":{"model":"iPhone15"},"user_input":{"title":"Crash on checkout"}}`)
	issueURL := "https://github.com/org/repo/issues/42"
	repoID := uuid.New()
	processedAt := now.Add(-5 * time.Minute)

	q := &fakeQuerier{
		app: db.App{ID: appID, OwnerID: owner},
		reports: map[uuid.UUID]db.Report{
			reportID: {
				ID:             internalID,
				AppID:          appID,
				ReportID:       reportID,
				InstallationID: uuid.New(),
				SessionID:      "sess-123",
				Trigger:        "shake",
				OccurredAt:     now.Add(-10 * time.Minute),
				ReceivedAt:     now.Add(-9 * time.Minute),
				ProcessedAt:    &processedAt,
				Payload:        payload,
			},
		},
		deliveries: map[uuid.UUID][]db.ReportDelivery{
			internalID: {
				{
					ReportID:            internalID,
					ProjectRepositoryID: repoID,
					URL:                 issueURL,
					CreatedAt:           processedAt,
				},
			},
		},
	}
	svc := NewService(q, nil)

	t.Run("happy path", func(t *testing.T) {
		res, err := svc.GetReport(ctx, owner, appID, reportID)
		if err != nil {
			t.Fatalf("GetReport() unexpected err: %v", err)
		}
		if res.ReportID != reportID {
			t.Errorf("ReportID = %v, want %v", res.ReportID, reportID)
		}
		if res.Trigger != "shake" {
			t.Errorf("Trigger = %q, want %q", res.Trigger, "shake")
		}
		if !bytes.Equal(res.Payload, payload) {
			t.Errorf("Payload mismatch: got %s, want %s", res.Payload, payload)
		}
		if res.Status != "delivered" {
			t.Errorf("Status = %q, want %q", res.Status, "delivered")
		}
		if len(res.Deliveries) != 1 {
			t.Fatalf("Deliveries len = %d, want 1", len(res.Deliveries))
		}
		if res.Deliveries[0].URL != issueURL {
			t.Errorf("Delivery URL = %q, want %q", res.Deliveries[0].URL, issueURL)
		}
		if res.Deliveries[0].ProjectRepositoryID != repoID {
			t.Errorf("Delivery ProjectRepositoryID = %v, want %v", res.Deliveries[0].ProjectRepositoryID, repoID)
		}
	})

	t.Run("missing report", func(t *testing.T) {
		_, err := svc.GetReport(ctx, owner, appID, uuid.New())
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound for missing report, got %v (kind %v)", err, kind(err))
		}
	})

	t.Run("cross-app / wrong owner isolation", func(t *testing.T) {
		otherAppID := uuid.New()
		if _, err := svc.GetReport(ctx, owner, otherAppID, reportID); kind(err) != apperr.KindNotFound {
			t.Errorf("expected KindNotFound for querying report under another app, got %v", err)
		}
		stranger := uuid.New()
		if _, err := svc.GetReport(ctx, stranger, appID, reportID); kind(err) != apperr.KindNotFound {
			t.Errorf("expected KindNotFound for unauthorized owner, got %v", err)
		}
	})
}

func TestRetryReport(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	appID := uuid.New()
	reportID := uuid.New()
	internalID := uuid.New()

	q := &fakeQuerier{
		app: db.App{ID: appID, OwnerID: owner},
		reports: map[uuid.UUID]db.Report{
			reportID: {
				ID:       internalID,
				AppID:    appID,
				ReportID: reportID,
			},
		},
	}
	pub := &fakePublisher{}
	svc := NewService(q, pub)

	t.Run("happy path", func(t *testing.T) {
		pub.published = nil
		err := svc.RetryReport(ctx, owner, appID, reportID)
		if err != nil {
			t.Fatalf("RetryReport() unexpected err: %v", err)
		}
		if len(pub.published) != 1 {
			t.Fatalf("expected exactly 1 published message, got %d", len(pub.published))
		}
		if pub.published[0].ID != internalID {
			t.Errorf("published ID = %v, want %v", pub.published[0].ID, internalID)
		}
		if pub.published[0].AppID != appID {
			t.Errorf("published AppID = %v, want %v", pub.published[0].AppID, appID)
		}
	})

	t.Run("missing report", func(t *testing.T) {
		pub.published = nil
		err := svc.RetryReport(ctx, owner, appID, uuid.New())
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound for missing report, got %v (kind %v)", err, kind(err))
		}
		if len(pub.published) != 0 {
			t.Errorf("expected 0 published messages for missing report, got %d", len(pub.published))
		}
	})

	t.Run("wrong owner", func(t *testing.T) {
		pub.published = nil
		err := svc.RetryReport(ctx, uuid.New(), appID, reportID)
		if kind(err) != apperr.KindNotFound {
			t.Fatalf("expected KindNotFound for unauthorized owner, got %v (kind %v)", err, kind(err))
		}
		if len(pub.published) != 0 {
			t.Errorf("expected 0 published messages for unauthorized owner, got %d", len(pub.published))
		}
	})
}

