-- name: CreateLink :one
INSERT INTO task_links (task_id, relevance, url, title, subscribe, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ListLinksByTask :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.subscribe, l.created_at
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.task_id = $1 AND t.org_id = $2
ORDER BY l.created_at ASC;

-- name: DeleteLink :exec
DELETE FROM task_links WHERE id = $1;

-- name: FindLinksByURL :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.subscribe, l.created_at
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.url = $1 AND t.archived = FALSE AND t.org_id = $2
ORDER BY l.created_at DESC;

-- name: FindSubscribedLinksForUser :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.subscribe, l.created_at, t.org_id
FROM task_links l
JOIN tasks t ON l.task_id = t.id
JOIN org_members om ON t.org_id = om.org_id
WHERE l.url = $1 AND l.subscribe = TRUE AND t.archived = FALSE AND om.user_id = $2
ORDER BY t.org_id, l.created_at DESC;
