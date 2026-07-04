-- Seed a fixed dev API key so the local runner can authenticate against the
-- --no-auth server.
--
-- Even with --no-auth the server validates any Bearer token it receives against
-- the keys table (the dev-user bypass only applies to requests with NO auth
-- header), and the runner always sends its key. So a real key row must exist,
-- owned by the dev user's org. The raw key (xat_dev) is stored as its SHA-256
-- hex digest, matching apiauth.HashKey.
--
-- Idempotent: `mise run dev` may run repeatedly against a persistent volume.
INSERT INTO keys (id, name, token_hash, org_id)
SELECT
    gen_random_uuid(),
    'dev-runner',
    encode(sha256('xat_dev'::bytea), 'hex'),
    u.default_org_id
FROM users u
WHERE u.id = 'dev'
  AND u.default_org_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM keys WHERE token_hash = encode(sha256('xat_dev'::bytea), 'hex')
  );
