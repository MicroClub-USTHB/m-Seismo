-- name: GetAppTrackerCredentials :one
SELECT * FROM app_tracker_credentials
WHERE app_id = $1 AND tracker = $2;

-- name: UpsertAppTrackerCredentials :one
INSERT INTO app_tracker_credentials (app_id, tracker, client_id, client_secret_ciphertext)
VALUES ($1, $2, $3, $4)
ON CONFLICT (app_id, tracker) DO UPDATE
SET client_id = excluded.client_id,
    client_secret_ciphertext = excluded.client_secret_ciphertext,
    updated_at = now()
RETURNING *;

-- name: DeleteAppTrackerCredentials :execrows
DELETE FROM app_tracker_credentials
WHERE app_id = $1 AND tracker = $2;
