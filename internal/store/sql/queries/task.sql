-- name: CreateTask :one
INSERT INTO tasks (name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id;

-- name: GetTask :one
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE id = $1 AND owner = $2;

-- name: HasTask :one
SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1 AND owner = $2);

-- name: ListTasks :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE status != 'archived' AND owner = $1
ORDER BY created_at DESC;

-- name: ListTaskChildren :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE parent = $1 AND owner = $2
ORDER BY created_at DESC;

-- name: ListTasksForRunner :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, owner, created_at, updated_at
FROM tasks
WHERE runner = $1 AND owner = $2 AND command != '' AND status != 'archived'
ORDER BY created_at DESC;

-- name: ListTasksByEvent :many
SELECT t.id, t.name, t.parent, t.runner, t.workspace, t.instructions, t.status, t.command, t.version, t.owner, t.created_at, t.updated_at
FROM tasks t
JOIN event_tasks et ON t.id = et.task_id
WHERE et.event_id = $1
ORDER BY t.created_at DESC;

-- name: UpdateTask :exec
UPDATE tasks
SET name = $1, parent = $2, runner = $3, workspace = $4, instructions = $5, status = $6, command = $7, version = $8, updated_at = $9
WHERE id = $10 AND owner = $11;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = $1 AND owner = $2;
