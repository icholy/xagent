-- +goose Up

CREATE TABLE tasks (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    parent        BIGINT NOT NULL DEFAULT 0,
    runner        TEXT NOT NULL,
    workspace     TEXT NOT NULL,
    instructions  TEXT NOT NULL,
    status        TEXT NOT NULL,
    command       TEXT NOT NULL DEFAULT '',
    version       BIGINT NOT NULL DEFAULT 0,
    owner         TEXT NOT NULL,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_tasks_owner ON tasks(owner);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_parent ON tasks(parent);
CREATE INDEX idx_tasks_runner_status ON tasks(runner, status);

CREATE TABLE logs (
    id         BIGSERIAL PRIMARY KEY,
    task_id    BIGINT NOT NULL,
    type       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);
CREATE INDEX idx_logs_task_id ON logs(task_id);

CREATE TABLE task_links (
    id         BIGSERIAL PRIMARY KEY,
    task_id    BIGINT NOT NULL,
    relevance  TEXT NOT NULL,
    url        TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    notify     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);
CREATE INDEX idx_task_links_task_id ON task_links(task_id);
CREATE INDEX idx_task_links_url ON task_links(url);

CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    description TEXT NOT NULL,
    data        TEXT NOT NULL,
    url         TEXT NOT NULL DEFAULT '',
    owner       TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_events_url ON events(url);
CREATE INDEX idx_events_owner ON events(owner);

CREATE TABLE event_tasks (
    event_id BIGINT NOT NULL,
    task_id  BIGINT NOT NULL,
    PRIMARY KEY (event_id, task_id),
    FOREIGN KEY (event_id) REFERENCES events(id),
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);
CREATE INDEX idx_event_tasks_task_id ON event_tasks(task_id);

CREATE TABLE workspaces (
    id         BIGSERIAL PRIMARY KEY,
    runner_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    owner      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_workspaces_runner_id ON workspaces(runner_id);
CREATE INDEX idx_workspaces_owner ON workspaces(owner);

-- +goose Down

DROP TABLE event_tasks;
DROP TABLE events;
DROP TABLE task_links;
DROP TABLE logs;
DROP TABLE workspaces;
DROP TABLE tasks;
