-- migrate:up
ALTER TABLE task_links ADD COLUMN routing_url TEXT NOT NULL DEFAULT '';

-- Existing rows already stored the canonical parent URL (the old contract),
-- so seeding routing_url = url preserves all current matches.
UPDATE task_links SET routing_url = url WHERE routing_url = '';

CREATE INDEX idx_task_links_routing_url ON task_links (routing_url);

-- The url-based indexes only backed FindLinksByURL / FindEventsByURL, both
-- removed in this change. Routing now matches on routing_url instead.
DROP INDEX IF EXISTS idx_task_links_url;
DROP INDEX IF EXISTS idx_events_url;

-- migrate:down
CREATE INDEX idx_events_url ON events (url);
CREATE INDEX idx_task_links_url ON task_links (url);
DROP INDEX IF EXISTS idx_task_links_routing_url;
ALTER TABLE task_links DROP COLUMN IF EXISTS routing_url;
