-- name: ListAppObservabilityProviders :many
SELECT * FROM app_observability_providers
WHERE app_id = $1
ORDER BY provider;

-- name: GetAppObservabilityProvider :one
SELECT * FROM app_observability_providers
WHERE app_id = $1 AND provider = $2;

-- name: SetAppObservabilityProvider :one
INSERT INTO app_observability_providers (app_id, provider, settings, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (app_id, provider) DO UPDATE
SET settings = EXCLUDED.settings, updated_at = now()
RETURNING *;

-- name: DeleteAppObservabilityProvider :exec
DELETE FROM app_observability_providers
WHERE app_id = $1 AND provider = $2;

-- name: DeleteAllAppObservabilityProviders :exec
DELETE FROM app_observability_providers
WHERE app_id = $1;
