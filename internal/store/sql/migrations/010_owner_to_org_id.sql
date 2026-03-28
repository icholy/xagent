-- +goose Up

-- tasks: rename owner to org_id, change type to BIGINT
ALTER TABLE tasks ADD COLUMN org_id BIGINT;
UPDATE tasks SET org_id = owner::BIGINT;
ALTER TABLE tasks ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE tasks DROP COLUMN owner;
DROP INDEX IF EXISTS idx_tasks_owner;
CREATE INDEX idx_tasks_org_id ON tasks(org_id);

-- events: rename owner to org_id, change type to BIGINT
ALTER TABLE events ADD COLUMN org_id BIGINT;
UPDATE events SET org_id = owner::BIGINT;
ALTER TABLE events ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE events DROP COLUMN owner;
DROP INDEX IF EXISTS idx_events_owner;
CREATE INDEX idx_events_org_id ON events(org_id);

-- workspaces: rename owner to org_id, change type to BIGINT
ALTER TABLE workspaces ADD COLUMN org_id BIGINT;
UPDATE workspaces SET org_id = owner::BIGINT;
ALTER TABLE workspaces ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE workspaces DROP COLUMN owner;
DROP INDEX IF EXISTS idx_workspaces_owner;
CREATE INDEX idx_workspaces_org_id ON workspaces(org_id);

-- keys: rename owner to org_id, change type to BIGINT
ALTER TABLE keys ADD COLUMN org_id BIGINT;
UPDATE keys SET org_id = owner::BIGINT;
ALTER TABLE keys ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE keys DROP COLUMN owner;
DROP INDEX IF EXISTS idx_keys_owner;
CREATE INDEX idx_keys_org_id ON keys(org_id);

-- orgs: change owner from stringified org ID to actual user ID (TEXT)
-- The owner was previously set to the org's own ID (stringified). Replace with the
-- actual user who owns the org, looked up from org_members where role='owner'.
UPDATE orgs SET owner = om.user_id
FROM org_members om WHERE om.org_id = orgs.id AND om.role = 'owner';

-- +goose Down

-- orgs: revert owner back to stringified org ID
UPDATE orgs SET owner = id::TEXT;

-- keys: restore owner column
DROP INDEX IF EXISTS idx_keys_org_id;
ALTER TABLE keys ADD COLUMN owner TEXT NOT NULL DEFAULT '';
UPDATE keys SET owner = org_id::TEXT;
ALTER TABLE keys DROP COLUMN org_id;
CREATE INDEX idx_keys_owner ON keys(owner);

-- workspaces: restore owner column
DROP INDEX IF EXISTS idx_workspaces_org_id;
ALTER TABLE workspaces ADD COLUMN owner TEXT NOT NULL DEFAULT '';
UPDATE workspaces SET owner = org_id::TEXT;
ALTER TABLE workspaces DROP COLUMN org_id;
CREATE INDEX idx_workspaces_owner ON workspaces(owner);

-- events: restore owner column
DROP INDEX IF EXISTS idx_events_org_id;
ALTER TABLE events ADD COLUMN owner TEXT NOT NULL DEFAULT '';
UPDATE events SET owner = org_id::TEXT;
ALTER TABLE events DROP COLUMN org_id;
CREATE INDEX idx_events_owner ON events(owner);

-- tasks: restore owner column
DROP INDEX IF EXISTS idx_tasks_org_id;
ALTER TABLE tasks ADD COLUMN owner TEXT NOT NULL DEFAULT '';
UPDATE tasks SET owner = org_id::TEXT;
ALTER TABLE tasks DROP COLUMN org_id;
CREATE INDEX idx_tasks_owner ON tasks(owner);
