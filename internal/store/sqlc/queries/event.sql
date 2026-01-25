-- name: CreateEvent :execlastid
INSERT INTO events (description, data, url, owner, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetEvent :one
SELECT id, description, data, url, owner, created_at
FROM events
WHERE id = ? AND owner = ?;

-- name: HasEvent :one
SELECT EXISTS(SELECT 1 FROM events WHERE id = ? AND owner = ?);

-- name: ListEvents :many
SELECT id, description, data, url, owner, created_at
FROM events
WHERE owner = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: FindEventsByURL :many
SELECT id, description, data, url, owner, created_at
FROM events
WHERE url = ?
ORDER BY created_at DESC;

-- name: DeleteEventTasks :exec
DELETE FROM event_tasks WHERE event_id = ?;

-- name: DeleteEvent :exec
DELETE FROM events WHERE id = ? AND owner = ?;

-- name: AddEventTask :exec
INSERT OR IGNORE INTO event_tasks (event_id, task_id) VALUES (?, ?);

-- name: RemoveEventTask :exec
DELETE FROM event_tasks WHERE event_id = ? AND task_id = ?;

-- name: ListEventTasks :many
SELECT et.task_id
FROM event_tasks et
JOIN tasks t ON et.task_id = t.id
WHERE et.event_id = ? AND t.owner = ?;

-- name: ListEventsByTask :many
SELECT e.id, e.description, e.data, e.url, e.owner, e.created_at
FROM events e
JOIN event_tasks et ON e.id = et.event_id
JOIN tasks t ON et.task_id = t.id
WHERE et.task_id = ? AND t.owner = ?
ORDER BY e.created_at DESC;
