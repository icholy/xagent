-- migrate:up

-- Events become task-scoped: an external event is now stored as one row per
-- subscribed task (each carrying its own task_id) instead of one org-scoped row
-- fanned out through the event_tasks junction. Per the unified-task-event-stream
-- proposal there is no backfill for existing rows, so clear the old org-scoped
-- events before adding the NOT NULL task_id column.
DELETE FROM events;

ALTER TABLE events ADD COLUMN task_id bigint NOT NULL;
ALTER TABLE events
    ADD CONSTRAINT fk_events_task_id FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
CREATE INDEX idx_events_task_id ON events (task_id);

DROP TABLE event_tasks;

-- migrate:down

CREATE TABLE event_tasks (
    event_id bigint NOT NULL,
    task_id bigint NOT NULL,
    CONSTRAINT event_tasks_pkey PRIMARY KEY (event_id, task_id)
);
ALTER TABLE event_tasks
    ADD CONSTRAINT event_tasks_event_id_fkey FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE;
ALTER TABLE event_tasks
    ADD CONSTRAINT event_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
CREATE INDEX idx_event_tasks_task_id ON event_tasks (task_id);

DROP INDEX IF EXISTS idx_events_task_id;
ALTER TABLE events DROP CONSTRAINT IF EXISTS fk_events_task_id;
ALTER TABLE events DROP COLUMN IF EXISTS task_id;
