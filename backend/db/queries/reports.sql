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

-- name: GetReport :one
SELECT * FROM reports
WHERE id = $1;

-- name: MarkReportProcessed :exec
UPDATE reports
SET processed_at = now()
WHERE id = $1 AND processed_at IS NULL;

-- name: ListReportDeliveries :many
SELECT * FROM report_deliveries
WHERE report_id = $1;

-- name: CreateReportDelivery :exec
INSERT INTO report_deliveries (report_id, project_repository_id, issue_id, url)
VALUES ($1, $2, $3, $4)
ON CONFLICT (report_id, project_repository_id) DO NOTHING;

-- name: ListReportAttachmentNames :many
SELECT name FROM report_attachments
WHERE report_id = $1
ORDER BY name;

-- name: ListReportDeliveryProjects :many
-- The projects that already got the report's issue, whichever of their
-- repositories it went to.
SELECT DISTINCT pr.project_id
FROM report_deliveries d
JOIN project_repositories pr ON pr.id = d.project_repository_id
WHERE d.report_id = $1;
