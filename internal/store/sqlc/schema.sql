-- Database schema (used by sqlc and embedded for runtime migrations)

CREATE TABLE IF NOT EXISTS tasks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL DEFAULT '',
    parent        INTEGER NOT NULL DEFAULT 0,
    runner        TEXT NOT NULL,
    workspace     TEXT NOT NULL,
    instructions  TEXT NOT NULL,
    status        TEXT NOT NULL,
    command       TEXT NOT NULL DEFAULT '',
    version       INTEGER NOT NULL DEFAULT 0,
    owner         TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    INTEGER NOT NULL,
    type       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);
CREATE INDEX IF NOT EXISTS idx_logs_task_id ON logs(task_id);

CREATE TABLE IF NOT EXISTS task_links (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    INTEGER NOT NULL,
    relevance  TEXT NOT NULL,
    url        TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    notify     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);
CREATE INDEX IF NOT EXISTS idx_task_links_task_id ON task_links(task_id);

CREATE TABLE IF NOT EXISTS events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    description TEXT NOT NULL,
    data        TEXT NOT NULL,
    url         TEXT NOT NULL DEFAULT '',
    owner       TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_events_url ON events(url);
CREATE INDEX IF NOT EXISTS idx_events_owner ON events(owner);

CREATE TABLE IF NOT EXISTS event_tasks (
    event_id INTEGER NOT NULL,
    task_id  INTEGER NOT NULL,
    PRIMARY KEY (event_id, task_id),
    FOREIGN KEY (event_id) REFERENCES events(id),
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);
CREATE INDEX IF NOT EXISTS idx_event_tasks_task_id ON event_tasks(task_id);

CREATE TABLE IF NOT EXISTS workspaces (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    runner_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    owner      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_workspaces_runner_id ON workspaces(runner_id);
CREATE INDEX IF NOT EXISTS idx_workspaces_owner ON workspaces(owner);
