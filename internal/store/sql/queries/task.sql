-- name: CreateTask :one
INSERT INTO tasks (name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive, shell_session)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id;

-- name: GetTask :one
SELECT id, name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive, shell_session
FROM tasks
WHERE id = $1 AND org_id = $2;

-- name: GetTaskForUpdate :one
SELECT id, name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive, shell_session
FROM tasks
WHERE id = $1 AND org_id = $2
FOR UPDATE;

-- name: ListTasks :many
SELECT id, name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive, shell_session
FROM tasks
WHERE archived = FALSE AND org_id = $1
ORDER BY created_at DESC;

-- name: ListTasksForRunner :many
SELECT id, name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive, shell_session
FROM tasks
WHERE runner = $1 AND org_id = $2 AND command != 0 AND archived = FALSE
ORDER BY created_at DESC;

-- name: UpdateTask :exec
UPDATE tasks
SET name = $1, runner = $2, workspace = $3, status = $4, command = $5, version = $6, updated_at = $7, archived = $8, auto_archive = $9, shell_session = $10
WHERE id = $11 AND org_id = $12;

-- name: ClearShellSession :exec
UPDATE tasks
SET shell_session = ''
WHERE shell_session = $1 AND org_id = $2;

-- name: ListTasksDueForArchive :many
SELECT id, version, org_id
FROM tasks
WHERE archived = FALSE
  AND auto_archive <> 0
  AND command = 0
  AND status IN (5, 6, 7)
  AND updated_at + (INTERVAL '1 microsecond' * auto_archive) < (NOW() AT TIME ZONE 'UTC')
ORDER BY updated_at
LIMIT $1;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = $1 AND org_id = $2;
