-- migrate:up
ALTER TABLE task_links ADD COLUMN routing_key TEXT NOT NULL DEFAULT '';

-- Existing rows already stored the canonical parent URL (the old contract),
-- so seeding routing_key = url preserves all current matches.
UPDATE task_links SET routing_key = url WHERE routing_key = '';

CREATE INDEX idx_task_links_routing_key ON task_links (routing_key);

-- The url-based indexes only backed FindLinksByURL / FindEventsByURL, both
-- removed in this change. Routing now matches on routing_key instead.
DROP INDEX IF EXISTS idx_task_links_url;
DROP INDEX IF EXISTS idx_events_url;

-- migrate:down
CREATE INDEX idx_events_url ON events (url);
CREATE INDEX idx_task_links_url ON task_links (url);
DROP INDEX IF EXISTS idx_task_links_routing_key;
ALTER TABLE task_links DROP COLUMN IF EXISTS routing_key;
