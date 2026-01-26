-- name: CreateWorkspace :exec
INSERT INTO workspaces (runner_id, name, owner)
VALUES ($1, $2, $3);

-- name: ListWorkspacesByOwner :many
SELECT id, runner_id, name, owner, updated_at
FROM workspaces
WHERE owner = $1
ORDER BY name ASC;

-- name: DeleteWorkspacesByRunner :exec
DELETE FROM workspaces
WHERE runner_id = $1 AND owner = $2;

-- name: ClearWorkspacesByOwner :exec
DELETE FROM workspaces
WHERE owner = $1;
