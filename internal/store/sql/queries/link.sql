-- name: CreateLink :one
-- routing_url defaults to url when the caller doesn't supply one, mirroring the
-- migration's `routing_url = url` backfill. Until callers derive it (see
-- proposals/draft/link-routing-url.md §2) this keeps FindSubscribedLinksForOrgs
-- matching exactly as it did before the column existed.
INSERT INTO task_links (task_id, relevance, url, routing_url, title, subscribe, created_at)
VALUES (
    sqlc.arg(task_id),
    sqlc.arg(relevance),
    sqlc.arg(url),
    COALESCE(NULLIF(sqlc.arg(routing_url)::text, ''), sqlc.arg(url)),
    sqlc.arg(title),
    sqlc.arg(subscribe),
    sqlc.arg(created_at)
)
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
