-- name: LinkProjectRepository :one
INSERT INTO project_repositories (project_id, tracker, repository, is_default)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListProjectRepositories :many
SELECT * FROM project_repositories
WHERE project_id = $1
ORDER BY created_at;

-- name: GetDefaultProjectRepository :one
SELECT * FROM project_repositories
WHERE project_id = $1 AND is_default;

-- name: UnlinkProjectRepository :exec
DELETE FROM project_repositories
WHERE id = $1;
