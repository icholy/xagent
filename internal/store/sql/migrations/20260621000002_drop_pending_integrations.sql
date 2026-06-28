-- migrate:up

DROP TABLE IF EXISTS pending_integrations;

-- migrate:down

CREATE TABLE pending_integrations (
    type        TEXT NOT NULL,
    external_id TEXT NOT NULL,
    options     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (type, external_id)
);
