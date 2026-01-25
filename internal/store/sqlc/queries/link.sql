-- name: CreateLink :execlastid
INSERT INTO task_links (task_id, relevance, url, title, notify, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListLinksByTask :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.notify, l.created_at
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.task_id = ? AND t.owner = ?
ORDER BY l.created_at ASC;

-- name: DeleteLink :exec
DELETE FROM task_links WHERE id = ?;

-- name: FindLinksByURL :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.notify, l.created_at
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.url = ? AND t.status != 'archived' AND t.owner = ?
ORDER BY l.created_at DESC;
