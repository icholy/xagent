-- migrate:up

-- schedules.version was written on every mutation but never read: the concurrency
-- guarantees come from FOR UPDATE SKIP LOCKED in ClaimDueSchedules and the
-- FOR UPDATE row lock in GetScheduleForUpdate, so the column is dead state.
ALTER TABLE schedules DROP COLUMN version;

-- migrate:down

ALTER TABLE schedules ADD COLUMN version BIGINT NOT NULL DEFAULT 0;
