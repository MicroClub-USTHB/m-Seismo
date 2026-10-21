-- name: InsertReport :one
-- Returns no row when the app already sent a report with this report_id.
INSERT INTO reports (app_id, report_id, installation_id, session_id, trigger, occurred_at, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (app_id, report_id) DO NOTHING
RETURNING *;

-- name: GetReportByReportID :one
SELECT * FROM reports
WHERE app_id = $1 AND report_id = $2;

-- name: InsertReportAttachment :exec
INSERT INTO report_attachments (report_id, name, content_type, data)
VALUES ($1, $2, $3, $4);

-- name: MarkReportQueued :exec
UPDATE reports
SET queued_at = now()
WHERE id = $1 AND queued_at IS NULL;
