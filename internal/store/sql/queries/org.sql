-- name: CreateOrg :one
INSERT INTO orgs (name, owner_id, created_at)
VALUES ($1, $2, $3)
RETURNING id;

-- name: GetOrg :one
SELECT id, name, owner_id, created_at
FROM orgs
WHERE id = $1;

-- name: ListOrgsByUser :many
SELECT o.id, o.name, o.owner_id, o.created_at
FROM orgs o
JOIN org_members m ON o.id = m.org_id
WHERE m.user_id = $1
ORDER BY o.created_at ASC;

-- name: DeleteOrg :exec
DELETE FROM orgs WHERE id = $1 AND owner_id = $2;

-- name: CreateOrgMember :exec
INSERT INTO org_members (org_id, user_id)
VALUES ($1, $2)
ON CONFLICT (org_id, user_id) DO NOTHING;

-- name: RemoveOrgMember :exec
DELETE FROM org_members WHERE org_id = $1 AND user_id = $2;

-- name: ListOrgMembers :many
SELECT m.id, m.org_id, m.user_id, m.created_at, u.email, u.name
FROM org_members m
JOIN users u ON m.user_id = u.id
WHERE m.org_id = $1
ORDER BY m.created_at ASC;

-- name: IsOrgMember :one
SELECT EXISTS(SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2);

-- name: UpsertUser :exec
INSERT INTO users (id, email, name, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    updated_at = EXCLUDED.updated_at;

-- name: GetUser :one
SELECT id, email, name, default_org_id, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, name, default_org_id, created_at, updated_at
FROM users
WHERE email = $1;

-- name: UpdateUserDefaultOrg :exec
UPDATE users SET default_org_id = $1, updated_at = $2 WHERE id = $3;
