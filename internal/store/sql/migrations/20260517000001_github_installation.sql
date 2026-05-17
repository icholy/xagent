-- migrate:up

ALTER TABLE orgs ADD COLUMN github_installation_id BIGINT;
CREATE UNIQUE INDEX idx_orgs_github_installation_id ON orgs(github_installation_id);

-- migrate:down

DROP INDEX IF EXISTS idx_orgs_github_installation_id;
ALTER TABLE orgs DROP COLUMN IF EXISTS github_installation_id;
