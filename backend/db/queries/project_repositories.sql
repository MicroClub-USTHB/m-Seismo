-- name: LinkProjectRepository :one
INSERT INTO project_repositories (project_id, provider_connection_id, repository, is_default)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProjectRepository :one
SELECT * FROM project_repositories
WHERE id = $1 AND project_id = $2;

-- name: ListProjectRepositories :many
SELECT
    pr.*,
    pc.tracker,
    pc.account_login
FROM project_repositories pr
JOIN provider_connections pc ON pc.id = pr.provider_connection_id
WHERE pr.project_id = $1
ORDER BY pr.created_at;

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
-- Resolves one project repository to everything needed to open an issue in it.
SELECT
    pr.id AS project_repository_id,
    pr.project_id,
    pr.repository,
    pc.tracker,
    pc.external_id
FROM project_repositories pr
JOIN provider_connections pc ON pc.id = pr.provider_connection_id
WHERE pr.id = $1 AND pc.suspended_at IS NULL;

-- name: ListIssueTargetsForApp :many
-- The default repository of every project linked to the app.
SELECT
    pr.id AS project_repository_id,
    pr.project_id,
    pr.repository,
    pc.tracker,
    pc.external_id
FROM app_projects ap
JOIN project_repositories pr ON pr.project_id = ap.project_id AND pr.is_default
JOIN provider_connections pc ON pc.id = pr.provider_connection_id
WHERE ap.app_id = $1 AND pc.suspended_at IS NULL
ORDER BY ap.created_at;
