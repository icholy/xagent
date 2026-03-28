-- name: CreateWorkspace :exec
INSERT INTO workspaces (runner_id, name, org_id)
VALUES ($1, $2, $3);

-- name: ListWorkspacesByOrgID :many
SELECT id, runner_id, name, updated_at, org_id
FROM workspaces
WHERE org_id = $1
ORDER BY name ASC;

-- name: DeleteWorkspacesByRunner :exec
DELETE FROM workspaces
WHERE runner_id = $1 AND org_id = $2;

-- name: ClearWorkspacesByOrgID :exec
DELETE FROM workspaces
WHERE org_id = $1;
