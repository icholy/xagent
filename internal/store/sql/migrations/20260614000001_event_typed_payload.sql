-- migrate:up

-- Give events a typed payload (the first data-model increment of the
-- unified-task-event-stream proposal). The flat description/data/url columns are
-- replaced by a materialized `type` column (the set Event.payload oneof arm), a
-- per-event `wake` flag, and a `payload` jsonb holding the encoding/json of the
-- model payload struct. As with the prior task-scoping migration there is no
-- backfill, so clear the table before reshaping it.
DELETE FROM events;

DROP INDEX IF EXISTS idx_events_url;
DROP INDEX IF EXISTS idx_events_task_id;

ALTER TABLE events DROP COLUMN description;
ALTER TABLE events DROP COLUMN data;
ALTER TABLE events DROP COLUMN url;

ALTER TABLE events ADD COLUMN type text NOT NULL;
ALTER TABLE events ADD COLUMN wake boolean NOT NULL DEFAULT false;
ALTER TABLE events ADD COLUMN payload jsonb NOT NULL;

-- (task_id, id) powers a task's ordered stream read; idx_events_org_id (from the
-- initial migration) powers the org event feed.
CREATE INDEX idx_events_task_id_id ON events (task_id, id);

-- migrate:down

DROP INDEX IF EXISTS idx_events_task_id_id;

ALTER TABLE events DROP COLUMN type;
ALTER TABLE events DROP COLUMN wake;
ALTER TABLE events DROP COLUMN payload;

ALTER TABLE events ADD COLUMN description text NOT NULL;
ALTER TABLE events ADD COLUMN data text NOT NULL;
ALTER TABLE events ADD COLUMN url text NOT NULL DEFAULT ''::text;

CREATE INDEX idx_events_url ON events (url);
CREATE INDEX idx_events_task_id ON events (task_id);
