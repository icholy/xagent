-- +goose Up

ALTER TABLE workspaces ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE workspaces DROP COLUMN description;
