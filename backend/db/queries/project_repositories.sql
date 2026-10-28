-- name: LinkProjectRepository :one
INSERT INTO project_repositories (project_id, tracker, repository, is_default)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProjectRepository :one
SELECT * FROM project_repositories
WHERE id = $1 AND project_id = $2;

-- name: ListProjectRepositories :many
SELECT * FROM project_repositories
WHERE project_id = $1
ORDER BY created_at;

-- name: ClearDefaultProjectRepository :exec
UPDATE project_repositories
SET is_default = false
WHERE project_id = $1 AND is_default;

-- name: SetDefaultProjectRepository :execrows
UPDATE project_repositories
SET is_default = true
WHERE id = $1 AND project_id = $2;

-- name: UnlinkProjectRepository :execrows
DELETE FROM project_repositories
WHERE id = $1 AND project_id = $2;

-- name: GetIssueTarget :one
-- One project repository, provided the app is linked to its project. The
-- credentials come from the app's connections, resolved by the caller.
SELECT
    pr.id AS project_repository_id,
    pr.project_id,
    pr.tracker,
    pr.repository
FROM project_repositories pr
JOIN app_projects ap ON ap.project_id = pr.project_id
WHERE pr.id = $1 AND ap.app_id = $2;

-- name: ListIssueTargetsForApp :many
-- The default repository of every project linked to the app.
SELECT
    pr.id AS project_repository_id,
    pr.project_id,
    pr.tracker,
    pr.repository
FROM app_projects ap
JOIN project_repositories pr ON pr.project_id = ap.project_id AND pr.is_default
WHERE ap.app_id = $1
ORDER BY ap.created_at;
