-- +goose Up
ALTER TABLE orgs ADD COLUMN routing_rules JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE orgs DROP COLUMN routing_rules;
