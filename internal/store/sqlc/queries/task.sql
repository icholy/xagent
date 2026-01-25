-- name: CreateTask :execlastid
INSERT INTO tasks (name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTask :one
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE id = ? AND owner = ?;

-- name: HasTask :one
SELECT EXISTS(SELECT 1 FROM tasks WHERE id = ? AND owner = ?);

-- name: ListTasks :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE status != 'archived' AND owner = ?
ORDER BY created_at DESC;

-- name: ListTaskChildren :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE parent = ? AND owner = ?
ORDER BY created_at DESC;

-- name: ListTasksForRunner :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE runner = ? AND owner = ? AND command != '' AND status != 'archived'
ORDER BY created_at DESC;

-- name: ListTasksByEvent :many
SELECT t.id, t.name, t.parent, t.runner, t.workspace, t.instructions, t.status, t.command, t.version, t.owner, t.created_at, t.updated_at
FROM tasks t
JOIN event_tasks et ON t.id = et.task_id
WHERE et.event_id = ?
ORDER BY t.created_at DESC;

-- name: UpdateTask :exec
UPDATE tasks
SET name = ?, parent = ?, runner = ?, workspace = ?, instructions = ?, status = ?, command = ?, version = ?, updated_at = ?
WHERE id = ? AND owner = ?;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = ? AND owner = ?;
