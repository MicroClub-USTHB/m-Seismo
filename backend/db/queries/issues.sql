-- name: CreateIssue :one
INSERT INTO issues (project_repository_id, app_id, provider_connection_id, external_id, url, title, fingerprint)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetOpenIssueByFingerprint :one
SELECT * FROM issues
WHERE project_repository_id = $1 AND fingerprint = $2 AND state = 'open'
ORDER BY created_at DESC
LIMIT 1;

-- name: SetIssueState :exec
UPDATE issues
SET state = $2,
    updated_at = now()
WHERE id = $1;

-- name: SetIssueStateByExternalID :execrows
-- Sets the state of the issue m-Seismo opened on the tracker as
-- external_id, e.g. after the tracker said it was closed. References are
-- compared without case, like GitHub compares repository names.
UPDATE issues i
SET state = sqlc.arg(state),
    updated_at = now()
FROM project_repositories pr
WHERE pr.id = i.project_repository_id
  AND pr.tracker = sqlc.arg(tracker)
  AND lower(i.external_id) = lower(sqlc.arg(external_id))
  AND i.state <> sqlc.arg(state);
