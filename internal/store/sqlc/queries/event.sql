-- name: CreateEvent :one
INSERT INTO events (description, data, url, owner, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: GetEvent :one
SELECT id, description, data, url, owner, created_at
FROM events
WHERE id = $1 AND owner = $2;

-- name: HasEvent :one
SELECT EXISTS(SELECT 1 FROM events WHERE id = $1 AND owner = $2);

-- name: ListEvents :many
SELECT id, description, data, url, owner, created_at
FROM events
WHERE owner = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: FindEventsByURL :many
SELECT id, description, data, url, owner, created_at
FROM events
WHERE url = $1
ORDER BY created_at DESC;

-- name: DeleteEventTasks :exec
DELETE FROM event_tasks WHERE event_id = $1;

-- name: DeleteEvent :exec
DELETE FROM events WHERE id = $1 AND owner = $2;

-- name: AddEventTask :exec
INSERT INTO event_tasks (event_id, task_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveEventTask :exec
DELETE FROM event_tasks WHERE event_id = $1 AND task_id = $2;

-- name: ListEventTasks :many
SELECT et.task_id
FROM event_tasks et
JOIN tasks t ON et.task_id = t.id
WHERE et.event_id = $1 AND t.owner = $2;

-- name: ListEventsByTask :many
SELECT e.id, e.description, e.data, e.url, e.owner, e.created_at
FROM events e
JOIN event_tasks et ON e.id = et.event_id
JOIN tasks t ON et.task_id = t.id
WHERE et.task_id = $1 AND t.owner = $2
ORDER BY e.created_at DESC;
