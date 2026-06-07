-- migrate:up

-- Rename the archive_after column to auto_archive. Pure rename; the semantics
-- are unchanged (microsecond duration after a task reaches a terminal status;
-- 0 = never, negative = immediate, positive = delay).
ALTER TABLE tasks RENAME COLUMN archive_after TO auto_archive;

DROP INDEX IF EXISTS idx_tasks_archive_due;
CREATE INDEX idx_tasks_archive_due
    ON tasks (updated_at)
    WHERE archived = FALSE AND auto_archive <> 0;

-- migrate:down

ALTER TABLE tasks RENAME COLUMN auto_archive TO archive_after;

DROP INDEX IF EXISTS idx_tasks_archive_due;
CREATE INDEX idx_tasks_archive_due
    ON tasks (updated_at)
    WHERE archived = FALSE AND archive_after <> 0;
