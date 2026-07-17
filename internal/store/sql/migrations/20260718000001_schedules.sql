-- migrate:up

CREATE TABLE schedules (
    id           BIGSERIAL PRIMARY KEY,
    org_id       BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    created_by   TEXT   NOT NULL REFERENCES users(id),  -- who set it up (attribution only)
    name         TEXT   NOT NULL DEFAULT '',

    -- Task template. Same shape CreateTask takes.
    workspace    TEXT   NOT NULL,
    runner       TEXT   NOT NULL,
    namespace    TEXT   NOT NULL DEFAULT '',
    instructions JSONB  NOT NULL DEFAULT '[]',  -- [{text, url}], seeded as instruction events
    auto_archive BIGINT NOT NULL DEFAULT 0,     -- microseconds; passed through to each task

    -- Schedule spec.
    cron_expr    TEXT   NOT NULL,
    timezone     TEXT   NOT NULL DEFAULT 'UTC',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,

    -- Scheduler bookkeeping.
    next_run_at  TIMESTAMP,                     -- next fire time (UTC); NULL when disabled/paused
    last_run_at  TIMESTAMP,                     -- last time we fired
    last_task_id BIGINT REFERENCES tasks(id) ON DELETE SET NULL,  -- most recent run, for the UI
    version      BIGINT NOT NULL DEFAULT 0,     -- optimistic-concurrency guard, mirrors tasks.version

    created_at   TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at   TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

-- The scheduler's claim query scans only enabled, due rows. Partial index keeps it index-only
-- and tiny even with many paused/future schedules — same shape as idx_tasks_archive_due.
CREATE INDEX idx_schedules_due
    ON schedules (next_run_at)
    WHERE enabled = TRUE AND next_run_at IS NOT NULL;

-- Org-scoped list, matching idx_tasks_org_id.
CREATE INDEX idx_schedules_org_id ON schedules (org_id);

-- migrate:down

DROP TABLE IF EXISTS schedules;
