-- name: CreateLog :execlastid
INSERT INTO logs (task_id, type, content, created_at)
VALUES (?, ?, ?, ?);

-- name: ListLogsByTask :many
SELECT l.id, l.task_id, l.type, l.content, l.created_at
FROM logs l
JOIN tasks t ON l.task_id = t.id
WHERE l.task_id = ? AND t.owner = ?
ORDER BY l.created_at ASC;
