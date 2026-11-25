-- name: CreateApp :one
INSERT INTO apps (owner_id, name, platform, bundle_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetApp :one
SELECT * FROM apps
WHERE id = $1 AND owner_id = $2;

-- name: ListApps :many
SELECT * FROM apps
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: UpdateApp :one
UPDATE apps
SET name = $3,
    platform = $4,
    bundle_id = $5,
    retention_days = $6,
    updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteApp :execrows
DELETE FROM apps
WHERE id = $1 AND owner_id = $2;

-- name: CreateAppKey :one
INSERT INTO app_keys (app_id, kind, key_hash, key_prefix)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListAppKeys :many
SELECT * FROM app_keys
WHERE app_id = $1 AND kind = $2
ORDER BY created_at DESC;

-- name: RevokeAppKey :execrows
UPDATE app_keys
SET revoked_at = now()
WHERE id = $1 AND app_id = $2 AND kind = $3 AND revoked_at IS NULL;

-- name: GetActiveAppKeyByHash :one
SELECT * FROM app_keys
WHERE key_hash = $1 AND kind = $2 AND revoked_at IS NULL;

-- name: LinkAppProject :exec
INSERT INTO app_projects (app_id, project_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UnlinkAppProject :execrows
DELETE FROM app_projects
WHERE app_id = $1 AND project_id = $2;

-- name: ListAppProjects :many
SELECT p.* FROM projects p
JOIN app_projects ap ON ap.project_id = p.id
WHERE ap.app_id = $1
ORDER BY ap.created_at;
