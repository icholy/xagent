-- +goose Up
CREATE TABLE keys (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    token_hash TEXT NOT NULL,
    owner      TEXT NOT NULL,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_keys_owner ON keys(owner);
CREATE UNIQUE INDEX idx_keys_token_hash ON keys(token_hash);

-- +goose Down
DROP TABLE keys;
