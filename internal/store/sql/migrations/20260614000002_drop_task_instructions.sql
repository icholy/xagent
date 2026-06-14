-- migrate:up

-- Instructions move out of the tasks row and into the event stream as
-- `instruction` events (the instruction increment of the
-- unified-task-event-stream proposal). There is no denormalized replacement and
-- no backfill, consistent with the prior stream migrations — instruction reads
-- filter the stream by type.
ALTER TABLE tasks DROP COLUMN instructions;

-- migrate:down

ALTER TABLE tasks ADD COLUMN instructions text NOT NULL DEFAULT ''::text;
