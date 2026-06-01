-- name: CreateLink :one
INSERT INTO task_links (task_id, relevance, url, routing_url, title, subscribe, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: ListLinksByTask :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.subscribe, l.created_at, l.routing_url
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.task_id = $1 AND t.org_id = $2
ORDER BY l.created_at ASC;

-- name: DeleteLink :exec
DELETE FROM task_links WHERE id = $1;

-- name: FindSubscribedLinksForOrgs :many
SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.subscribe, l.created_at, l.routing_url, t.org_id
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.routing_url = sqlc.arg(routing_url) AND l.subscribe = TRUE AND t.archived = FALSE
  AND t.org_id = ANY(sqlc.arg(org_ids)::BIGINT[])
ORDER BY t.org_id, l.created_at DESC;
