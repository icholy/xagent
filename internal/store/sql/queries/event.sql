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
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE task_id = $1 AND org_id = $2
ORDER BY id DESC;
