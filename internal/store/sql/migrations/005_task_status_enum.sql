-- +goose Up
ALTER TABLE tasks ALTER COLUMN status TYPE INTEGER USING (
    CASE status
        WHEN 'pending' THEN 1
        WHEN 'running' THEN 2
        WHEN 'restarting' THEN 3
        WHEN 'cancelling' THEN 4
        WHEN 'completed' THEN 5
        WHEN 'failed' THEN 6
        WHEN 'cancelled' THEN 7
        ELSE 0
    END
);
ALTER TABLE tasks ALTER COLUMN command DROP DEFAULT;
ALTER TABLE tasks ALTER COLUMN command TYPE INTEGER USING (
    CASE command
        WHEN 'restart' THEN 1
        WHEN 'stop' THEN 2
        WHEN 'start' THEN 3
        ELSE 0
    END
);
ALTER TABLE tasks ALTER COLUMN command SET DEFAULT 0;

-- +goose Down
ALTER TABLE tasks ALTER COLUMN status TYPE TEXT USING (
    CASE status
        WHEN 1 THEN 'pending'
        WHEN 2 THEN 'running'
        WHEN 3 THEN 'restarting'
        WHEN 4 THEN 'cancelling'
        WHEN 5 THEN 'completed'
        WHEN 6 THEN 'failed'
        WHEN 7 THEN 'cancelled'
        ELSE 'pending'
    END
);
ALTER TABLE tasks ALTER COLUMN command DROP DEFAULT;
ALTER TABLE tasks ALTER COLUMN command TYPE TEXT USING (
    CASE command
        WHEN 1 THEN 'restart'
        WHEN 2 THEN 'stop'
        WHEN 3 THEN 'start'
        ELSE ''
    END
);
ALTER TABLE tasks ALTER COLUMN command SET DEFAULT '';
