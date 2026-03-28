-- name: CreateOrg :one
INSERT INTO orgs (name, owner, created_at, updated_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: SetOrgOwner :exec
UPDATE orgs SET owner = $2 WHERE id = $1;

-- name: GetOrg :one
SELECT id, name, owner, created_at, updated_at
FROM orgs
WHERE id = $1;

-- name: ListOrgsByMember :many
SELECT o.id, o.name, o.owner, o.created_at, o.updated_at
FROM orgs o
JOIN org_members om ON o.id = om.org_id
WHERE om.user_id = $1
ORDER BY o.name;

-- name: UpdateOrg :exec
UPDATE orgs SET
    name = $2,
    updated_at = $3
WHERE id = $1;

-- name: DeleteOrg :exec
DELETE FROM orgs WHERE id = $1;

-- name: AddOrgMember :exec
INSERT INTO org_members (org_id, user_id, role, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (org_id, user_id) DO NOTHING;

-- name: RemoveOrgMember :exec
DELETE FROM org_members
WHERE org_id = $1 AND user_id = $2;

-- name: ListOrgMembers :many
SELECT om.org_id, om.user_id, om.role, om.created_at
FROM org_members om
WHERE om.org_id = $1
ORDER BY om.created_at;

-- name: GetOrgMember :one
SELECT org_id, user_id, role, created_at
FROM org_members
WHERE org_id = $1 AND user_id = $2;

-- name: IsOrgMember :one
SELECT EXISTS(
    SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2
) AS is_member;
