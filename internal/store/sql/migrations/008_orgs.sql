-- +goose Up

CREATE TABLE orgs (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    owner      TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_orgs_owner ON orgs(owner);

CREATE TABLE org_members (
    org_id     BIGINT NOT NULL,
    user_id    TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (org_id, user_id),
    FOREIGN KEY (org_id) REFERENCES orgs(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX idx_org_members_user_id ON org_members(user_id);

ALTER TABLE users ADD COLUMN default_org_id BIGINT;

-- Create a personal org for each existing user and set it as default.
-- The org owner is set to the stringified org ID after insert via a CTE.
WITH new_orgs AS (
    INSERT INTO orgs (name, owner)
    SELECT name, id FROM users
    RETURNING id, owner AS user_id
)
UPDATE users SET default_org_id = new_orgs.id
FROM new_orgs WHERE users.id = new_orgs.user_id;

-- Update org owner from user ID to org ID (stringified).
UPDATE orgs SET owner = id::TEXT
WHERE owner IN (SELECT id FROM users);

-- Add org_members rows for the personal org owners.
INSERT INTO org_members (org_id, user_id, role)
SELECT o.id, u.id, 'owner'
FROM users u JOIN orgs o ON o.id = u.default_org_id;

-- Migrate existing owner columns from user IDs to org IDs.
UPDATE tasks SET owner = u.default_org_id::TEXT
FROM users u WHERE tasks.owner = u.id;

UPDATE events SET owner = u.default_org_id::TEXT
FROM users u WHERE events.owner = u.id;

UPDATE workspaces SET owner = u.default_org_id::TEXT
FROM users u WHERE workspaces.owner = u.id;

UPDATE keys SET owner = u.default_org_id::TEXT
FROM users u WHERE keys.owner = u.id;

-- +goose Down

-- Reverse owner migration (org ID back to user ID).
UPDATE keys SET owner = om.user_id
FROM org_members om WHERE keys.owner = om.org_id::TEXT AND om.role = 'owner';

UPDATE workspaces SET owner = om.user_id
FROM org_members om WHERE workspaces.owner = om.org_id::TEXT AND om.role = 'owner';

UPDATE events SET owner = om.user_id
FROM org_members om WHERE events.owner = om.org_id::TEXT AND om.role = 'owner';

UPDATE tasks SET owner = om.user_id
FROM org_members om WHERE tasks.owner = om.org_id::TEXT AND om.role = 'owner';

ALTER TABLE users DROP COLUMN default_org_id;
DROP TABLE org_members;
DROP TABLE orgs;
