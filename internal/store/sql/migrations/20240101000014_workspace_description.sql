-- migrate:up

ALTER TABLE workspaces ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- migrate:down

ALTER TABLE workspaces DROP COLUMN description;
