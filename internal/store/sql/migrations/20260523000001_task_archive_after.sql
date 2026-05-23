-- migrate:up

-- archive_after stores the auto-archive timeout in microseconds (so it round-trips
-- cleanly to and from Go's time.Duration via d.Microseconds()). NULL = never
-- auto-archive. PostgreSQL's interval precision is microseconds, so no info is
-- lost converting to an interval at query time.
ALTER TABLE tasks
    ADD COLUMN archive_after BIGINT;

CREATE INDEX idx_tasks_archive_due
    ON tasks (updated_at)
    WHERE archived = FALSE AND archive_after IS NOT NULL;

-- migrate:down

DROP INDEX IF EXISTS idx_tasks_archive_due;
ALTER TABLE tasks DROP COLUMN IF EXISTS archive_after;
