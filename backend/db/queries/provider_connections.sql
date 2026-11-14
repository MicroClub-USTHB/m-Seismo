-- name: UpsertProviderConnection :one
-- Connecting the same tracker account to the same app again refreshes the
-- account details. Connecting an account that another owner holds fails on
-- provider_connections_one_owner_per_account (SQLSTATE 23P01).
INSERT INTO provider_connections (owner_id, app_id, tracker, external_id, account_login, account_type)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (app_id, tracker, external_id) DO UPDATE
SET account_login = excluded.account_login,
    account_type = excluded.account_type,
    suspended_at = NULL,
    updated_at = now()
RETURNING *;

-- name: GetAppConnection :one
SELECT * FROM provider_connections
WHERE id = $1 AND app_id = $2 AND owner_id = $3;

-- name: GetOwnerConnection :one
SELECT * FROM provider_connections
WHERE id = $1 AND owner_id = $2;

-- name: ListAppConnections :many
SELECT * FROM provider_connections
WHERE app_id = $1 AND owner_id = $2
ORDER BY created_at;

-- name: ListOwnerConnections :many
SELECT * FROM provider_connections
WHERE owner_id = $1
ORDER BY created_at;

-- name: ListActiveConnectionsForApp :many
-- The connections the issue service may use for an app.
SELECT * FROM provider_connections
WHERE app_id = $1 AND suspended_at IS NULL
ORDER BY created_at;

-- name: DeleteAppConnection :execrows
DELETE FROM provider_connections
WHERE id = $1 AND app_id = $2 AND owner_id = $3;
