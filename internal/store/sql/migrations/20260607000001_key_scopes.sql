-- migrate:up
ALTER TABLE keys ADD COLUMN scopes TEXT[];

-- Backfill every existing key to the admin wildcard (authscope.AdminScope) so a
-- later enforcement phase leaves their effective access unchanged. The column is
-- nullable on purpose: ValidateKey treats a NULL/empty column as Admin() too, so
-- an un-backfilled row is never locked out.
UPDATE keys SET scopes = ARRAY['*.*'] WHERE scopes IS NULL;

-- migrate:down
ALTER TABLE keys DROP COLUMN IF EXISTS scopes;
