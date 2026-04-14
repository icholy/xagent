-- +goose Up
CREATE INDEX idx_users_default_org_id ON users(default_org_id);

-- +goose Down
DROP INDEX idx_users_default_org_id;
