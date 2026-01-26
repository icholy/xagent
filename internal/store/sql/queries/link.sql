-- name: CreateLink :one
INSERT INTO task_links (task_id, relevance, url, title, notify, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ListLinksByTask :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.notify, l.created_at
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.task_id = $1 AND t.owner = $2
ORDER BY l.created_at ASC;

-- name: DeleteLink :exec
DELETE FROM task_links WHERE id = $1;

-- name: FindLinksByURL :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.notify, l.created_at
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.url = $1 AND t.status != 'archived' AND t.owner = $2
ORDER BY l.created_at DESC;
