-- migrate:up
ALTER TABLE orgs ADD COLUMN routing_rules JSONB NOT NULL DEFAULT '[]';

-- migrate:down
ALTER TABLE orgs DROP COLUMN routing_rules;
