-- name: CreateLog :one
INSERT INTO logs (task_id, type, content, created_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: ListLogsByTask :many
SELECT l.id, l.task_id, l.type, l.content, l.created_at
FROM logs l
JOIN tasks t ON l.task_id = t.id
WHERE l.task_id = $1 AND t.org_id = $2
ORDER BY l.created_at ASC;
