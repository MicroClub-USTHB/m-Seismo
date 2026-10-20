-- name: CreateProject :one
INSERT INTO projects (owner_id, name, slug)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects
WHERE id = $1 AND owner_id = $2;

-- name: ListProjects :many
SELECT * FROM projects
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: UpdateProject :one
UPDATE projects
SET name = $3,
    slug = $4,
    updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteProject :execrows
DELETE FROM projects
WHERE id = $1 AND owner_id = $2;
