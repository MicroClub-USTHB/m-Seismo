-- name: CreateProject :one
INSERT INTO projects (name, slug, default_repository)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProjectByID :one
SELECT * FROM projects
WHERE id = $1;

-- name: GetProjectBySlug :one
SELECT * FROM projects
WHERE slug = $1;

-- name: ListProjects :many
SELECT * FROM projects
ORDER BY created_at DESC;

-- name: CreateProjectKey :one
INSERT INTO project_keys (project_id, key)
VALUES ($1, $2)
RETURNING *;

-- name: GetActiveProjectKey :one
SELECT * FROM project_keys
WHERE key = $1 AND revoked_at IS NULL;

-- name: RevokeProjectKey :exec
UPDATE project_keys
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;
