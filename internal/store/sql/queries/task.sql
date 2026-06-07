-- name: CreateTask :one
INSERT INTO tasks (name, parent, runner, workspace, instructions, status, command, version, org_id, archived, created_at, updated_at, auto_archive)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id;

-- name: GetTask :one
SELECT id, name, parent, runner, workspace, instructions, status, command, version, org_id, archived, created_at, updated_at, auto_archive
FROM tasks
WHERE id = $1 AND org_id = $2;

-- name: GetTaskForUpdate :one
SELECT id, name, parent, runner, workspace, instructions, status, command, version, org_id, archived, created_at, updated_at, auto_archive
FROM tasks
WHERE id = $1 AND org_id = $2
FOR UPDATE;

-- name: HasTask :one
SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1 AND org_id = $2);

-- name: ListTasks :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, org_id, archived, created_at, updated_at, auto_archive
FROM tasks
WHERE archived = FALSE AND org_id = $1
ORDER BY created_at DESC;

-- name: ListTaskChildren :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, org_id, archived, created_at, updated_at, auto_archive
FROM tasks
WHERE parent = $1 AND org_id = $2
ORDER BY created_at DESC;

-- name: ListTasksForRunner :many
SELECT id, name, parent, runner, workspace, instructions, status, command, version, org_id, archived, created_at, updated_at, auto_archive
FROM tasks
WHERE runner = $1 AND org_id = $2 AND command != 0 AND archived = FALSE
ORDER BY created_at DESC;

-- name: ListTasksByEvent :many
SELECT t.id, t.name, t.parent, t.runner, t.workspace, t.instructions, t.status, t.command, t.version, t.org_id, t.archived, t.created_at, t.updated_at, t.auto_archive
FROM tasks t
JOIN event_tasks et ON t.id = et.task_id
WHERE et.event_id = $1
ORDER BY t.created_at DESC;

-- name: UpdateTask :exec
UPDATE tasks
SET name = $1, parent = $2, runner = $3, workspace = $4, instructions = $5, status = $6, command = $7, version = $8, updated_at = $9, archived = $10, auto_archive = $11
WHERE id = $12 AND org_id = $13;

-- name: ListTasksDueForArchive :many
SELECT id, version, org_id
FROM tasks
WHERE archived = FALSE
  AND auto_archive <> 0
  AND command = 0
  AND status IN (5, 6, 7)
  AND updated_at + (INTERVAL '1 microsecond' * auto_archive) < NOW()
ORDER BY updated_at
LIMIT $1;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = $1 AND org_id = $2;
