-- +goose Up

ALTER TABLE orgs ADD COLUMN archived BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down

ALTER TABLE orgs DROP COLUMN archived;
