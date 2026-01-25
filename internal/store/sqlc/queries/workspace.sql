-- name: CreateWorkspace :exec
INSERT INTO workspaces (runner_id, name, owner)
VALUES (?, ?, ?);

-- name: ListWorkspacesByOwner :many
SELECT id, runner_id, name, owner, updated_at
FROM workspaces
WHERE owner = ?
ORDER BY name ASC;

-- name: DeleteWorkspacesByRunner :exec
DELETE FROM workspaces
WHERE runner_id = ? AND owner = ?;

-- name: ClearWorkspacesByOwner :exec
DELETE FROM workspaces
WHERE owner = ?;
