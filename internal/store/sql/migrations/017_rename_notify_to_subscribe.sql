-- +goose Up
ALTER TABLE task_links RENAME COLUMN notify TO subscribe;

-- +goose Down
ALTER TABLE task_links RENAME COLUMN subscribe TO notify;
