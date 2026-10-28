-- name: UpsertConnectionSecret :exec
-- Stores a connection's encrypted credentials, replacing earlier ones (a
-- re-connect, or a refreshed OAuth token).
INSERT INTO connection_secrets (provider_connection_id, ciphertext)
VALUES ($1, $2)
ON CONFLICT (provider_connection_id) DO UPDATE
SET ciphertext = excluded.ciphertext,
    updated_at = now();

-- name: GetConnectionSecret :one
SELECT ciphertext FROM connection_secrets
WHERE provider_connection_id = $1;
