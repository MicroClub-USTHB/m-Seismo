-- name: UpsertCrashlyticsConnection :one
INSERT INTO crashlytics_connections (project_id, google_email, refresh_token)
VALUES ($1, $2, $3)
ON CONFLICT (project_id) DO UPDATE
SET google_email = EXCLUDED.google_email,
    refresh_token = EXCLUDED.refresh_token,
    updated_at = now()
RETURNING *;

-- name: GetCrashlyticsConnection :one
SELECT * FROM crashlytics_connections
WHERE project_id = $1;

-- name: ListCrashlyticsConnections :many
SELECT * FROM crashlytics_connections
ORDER BY created_at;

-- name: SetCrashlyticsApps :execrows
UPDATE crashlytics_connections
SET apps = $2, updated_at = now()
WHERE project_id = $1;
