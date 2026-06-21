-- migrate:up

DROP INDEX IF EXISTS idx_orgs_github_installation_id;
CREATE INDEX idx_orgs_github_installation_id ON orgs(github_installation_id);

-- migrate:down

DROP INDEX IF EXISTS idx_orgs_github_installation_id;
CREATE UNIQUE INDEX idx_orgs_github_installation_id ON orgs(github_installation_id);
