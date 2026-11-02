-- name: InsertUser :one
INSERT INTO users (email, display_name)
VALUES ($1, $2)
RETURNING *;

-- name: FindUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY id DESC
LIMIT $1;
