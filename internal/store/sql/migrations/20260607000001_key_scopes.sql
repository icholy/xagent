-- migrate:up
-- Default every key to the admin wildcard (authscope.AdminScope) so a later
-- enforcement phase leaves their effective access unchanged. ADD COLUMN ...
-- DEFAULT backfills all existing rows, and the column default covers any future
-- insert that omits scopes, so a key never ends up without scopes and no runtime
-- fallback is needed. The column stays nullable for the Phase 4 default-deny work.
ALTER TABLE keys ADD COLUMN scopes TEXT[] DEFAULT ARRAY['*.*'];

-- migrate:down
ALTER TABLE keys DROP COLUMN IF EXISTS scopes;
