-- +goose Up
CREATE TABLE webhooks (
    uuid       UUID PRIMARY KEY,
    secret     TEXT NOT NULL,
    owner      TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_webhooks_owner ON webhooks(owner);

-- +goose Down
DROP TABLE webhooks;
