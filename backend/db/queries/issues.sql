-- name: CreateIssue :one
INSERT INTO issues (project_repository_id, app_id, external_id, url, title, fingerprint)
VALUES ($1, $2, $3, $4, $5, $6)
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
