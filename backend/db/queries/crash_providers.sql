-- name: GetAppCrashProvider :one
SELECT * FROM app_crash_providers WHERE app_id = $1;

-- name: SetAppCrashProvider :one
INSERT INTO app_crash_providers (app_id, provider, settings, credentials)
VALUES ($1, $2, $3, $4)
ON CONFLICT (app_id) DO UPDATE SET provider = EXCLUDED.provider,
    settings = EXCLUDED.settings, credentials = EXCLUDED.credentials, updated_at = now()
RETURNING *;

-- name: DeleteAppCrashProvider :exec
DELETE FROM app_crash_providers WHERE app_id = $1;
