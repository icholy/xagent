-- migrate:up

-- archive_after stores the auto-archive timeout in microseconds (so it round-trips
-- cleanly to and from Go's time.Duration via d.Microseconds()). PostgreSQL's
-- interval precision is microseconds, so no info is lost converting to an
-- interval at query time.
--
-- Sentinels:
--   = 0  never auto-archive (default)
--   < 0  archive immediately once the task reaches a terminal status
--   > 0  archive that long after the task reaches a terminal status
ALTER TABLE tasks
    ADD COLUMN archive_after BIGINT NOT NULL DEFAULT 0;

CREATE INDEX idx_tasks_archive_due
    ON tasks (updated_at)
    WHERE archived = FALSE AND archive_after <> 0;

-- migrate:down

DROP INDEX IF EXISTS idx_tasks_archive_due;
ALTER TABLE tasks DROP COLUMN IF EXISTS archive_after;
