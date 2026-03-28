-- +goose Up

-- Organisations table
CREATE TABLE orgs (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Organisation members table
CREATE TABLE org_members (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(org_id, user_id)
);

-- Users table (populated on login)
CREATE TABLE users (
    id             TEXT PRIMARY KEY,
    email          TEXT NOT NULL,
    name           TEXT NOT NULL,
    default_org_id BIGINT REFERENCES orgs(id),
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Data migration: create a personal org for each unique owner, remap owner columns
-- Step 1: Collect unique owner IDs across all tables
CREATE TEMP TABLE unique_owners AS
SELECT DISTINCT owner AS user_id FROM (
    SELECT owner FROM tasks
    UNION
    SELECT owner FROM events
    UNION
    SELECT owner FROM keys
    UNION
    SELECT owner FROM workspaces
    UNION
    SELECT owner FROM github_accounts
) all_owners;

-- Step 2: Create a personal org for each unique owner
INSERT INTO orgs (name, owner_id)
SELECT 'Personal', user_id FROM unique_owners;

-- Step 3: Add each owner as a member of their personal org
INSERT INTO org_members (org_id, user_id)
SELECT o.id, o.owner_id FROM orgs o;

-- Step 4: Update owner column in all tables from user_id to org_id
UPDATE tasks SET owner = (
    SELECT CAST(o.id AS TEXT)
    FROM orgs o WHERE o.owner_id = tasks.owner
);

UPDATE events SET owner = (
    SELECT CAST(o.id AS TEXT)
    FROM orgs o WHERE o.owner_id = events.owner
);

UPDATE keys SET owner = (
    SELECT CAST(o.id AS TEXT)
    FROM orgs o WHERE o.owner_id = keys.owner
);

UPDATE workspaces SET owner = (
    SELECT CAST(o.id AS TEXT)
    FROM orgs o WHERE o.owner_id = workspaces.owner
);

UPDATE github_accounts SET owner = (
    SELECT CAST(o.id AS TEXT)
    FROM orgs o WHERE o.owner_id = github_accounts.owner
);

DROP TABLE unique_owners;

-- +goose Down
-- Reverse the data migration is not feasible, just drop the new tables
DROP TABLE IF EXISTS org_members;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS orgs;
