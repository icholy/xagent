-- name: CreateEvent :one
INSERT INTO events (task_id, org_id, type, wake, payload, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: GetEvent :one
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE id = $1 AND org_id = $2;

-- name: ListEvents :many
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE org_id = $1
ORDER BY id DESC
LIMIT $2;

-- name: DeleteEvent :exec
DELETE FROM events WHERE id = $1 AND org_id = $2;

-- name: ListEventsByTask :many
-- A task's events in chronological stream order (ORDER BY id). The optional
-- types filter ($3) narrows to specific arms — an empty/nil array matches all
-- types — so the same query serves both the full timeline and the agent's brief
-- (instruction + external; see proposals/draft/unified-task-event-stream.md
-- "The agent's brief").
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE task_id = $1 AND org_id = $2
  AND (cardinality(sqlc.arg(types)::text[]) = 0 OR type = ANY(sqlc.arg(types)::text[]))
ORDER BY id;
