-- name: UpsertProviderConnection :one
-- Re-installing refreshes the account details. A connection already owned by
-- another owner is left untouched and no row is returned.
INSERT INTO provider_connections (owner_id, tracker, external_id, account_login, account_type)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tracker, external_id) DO UPDATE
SET account_login = excluded.account_login,
    account_type = excluded.account_type,
    suspended_at = NULL,
    updated_at = now()
WHERE provider_connections.owner_id = excluded.owner_id
RETURNING *;

-- name: GetProviderConnection :one
SELECT * FROM provider_connections
WHERE id = $1 AND owner_id = $2;

-- name: ListProviderConnections :many
SELECT * FROM provider_connections
WHERE owner_id = $1
ORDER BY created_at;

-- name: DeleteProviderConnection :execrows
DELETE FROM provider_connections
WHERE id = $1 AND owner_id = $2;
