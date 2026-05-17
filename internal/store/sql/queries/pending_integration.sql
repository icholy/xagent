-- name: UpsertPendingIntegration :exec
INSERT INTO pending_integrations (type, external_id, options, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (type, external_id) DO UPDATE SET
    options = EXCLUDED.options;

-- name: GetPendingIntegration :one
SELECT type, external_id, options, created_at
FROM pending_integrations
WHERE type = $1 AND external_id = $2;

-- name: DeletePendingIntegration :exec
DELETE FROM pending_integrations
WHERE type = $1 AND external_id = $2;
