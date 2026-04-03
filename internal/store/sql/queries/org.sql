-- name: CreateOrg :one
INSERT INTO orgs (name, owner, created_at, updated_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: GetOrg :one
SELECT id, name, owner, created_at, updated_at, archived
FROM orgs
WHERE id = $1;

-- name: ListOrgsByMember :many
SELECT o.id, o.name, o.owner, o.created_at, o.updated_at, o.archived
FROM orgs o
JOIN org_members om ON o.id = om.org_id
WHERE om.user_id = $1 AND o.archived = FALSE
ORDER BY o.name;

-- name: UpdateOrg :exec
UPDATE orgs SET
    name = $2,
    updated_at = $3
WHERE id = $1;

-- name: ArchiveOrg :exec
UPDATE orgs SET archived = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1;

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

-- name: ListOrgMembersWithUsers :many
SELECT om.org_id, om.user_id, om.role, om.created_at, u.email, u.name
FROM org_members om
JOIN users u ON om.user_id = u.id
WHERE om.org_id = $1
ORDER BY om.created_at;

-- name: GetOrgAtlassianWebhookSecret :one
SELECT atlassian_webhook_secret FROM orgs WHERE id = $1;

-- name: SetOrgAtlassianWebhookSecret :exec
UPDATE orgs SET atlassian_webhook_secret = $2 WHERE id = $1;

-- name: DestroyOrg :exec
DELETE FROM orgs WHERE id = $1;

-- name: IsOrgMember :one
SELECT EXISTS(
    SELECT 1 FROM org_members om
    JOIN orgs o ON o.id = om.org_id
    WHERE om.org_id = $1 AND om.user_id = $2 AND o.archived = FALSE
) AS is_member;

-- name: GetOrgRoutingRules :one
SELECT routing_rules FROM orgs WHERE id = $1;

-- name: SetOrgRoutingRules :exec
UPDATE orgs SET
    routing_rules = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: GetRoutingRulesByOrgs :many
SELECT id, routing_rules FROM orgs WHERE id = ANY($1::BIGINT[]);

