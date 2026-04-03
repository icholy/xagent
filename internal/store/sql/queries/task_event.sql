-- name: CreateTaskEvent :one
INSERT INTO task_events (task_id, type, content, meta, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: PollTaskEvents :many
SELECT id, task_id, type, content, meta, created_at
FROM task_events
WHERE task_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3;

-- name: DeleteTaskEventsByTask :exec
DELETE FROM task_events WHERE task_id = $1;
