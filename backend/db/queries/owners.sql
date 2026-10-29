-- name: CreateOwner :one
INSERT INTO owners (email, password_hash, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetOwnerByID :one
SELECT * FROM owners
WHERE id = $1;

-- name: GetOwnerByEmail :one
SELECT * FROM owners
WHERE lower(email) = lower(@email::text);

-- name: ListOwners :many
SELECT * FROM owners
ORDER BY created_at DESC;

-- name: UpdateOwner :one
UPDATE owners
SET email = $2,
    password_hash = $3,
    name = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteOwner :execrows
DELETE FROM owners
WHERE id = $1;
