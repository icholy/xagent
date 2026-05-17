
CREATE TABLE users (
    id              TEXT PRIMARY KEY,
    email           TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    github_user_id  BIGINT,
    github_username TEXT,
    atlassian_account_id TEXT,
    atlassian_username TEXT NOT NULL DEFAULT '',
    default_org_id  BIGINT,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_users_email ON users(email);
CREATE UNIQUE INDEX idx_users_github_user_id ON users(github_user_id);
CREATE UNIQUE INDEX idx_users_atlassian_account_id ON users(atlassian_account_id);

CREATE TABLE orgs (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    owner      TEXT NOT NULL,
    archived   BOOLEAN NOT NULL DEFAULT FALSE,
    atlassian_webhook_secret TEXT NOT NULL DEFAULT '',
    routing_rules JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_orgs_owner FOREIGN KEY (owner) REFERENCES users(id)
);
CREATE INDEX idx_orgs_owner ON orgs(owner);

ALTER TABLE users ADD CONSTRAINT fk_users_default_org_id FOREIGN KEY (default_org_id) REFERENCES orgs(id) ON DELETE SET NULL;

CREATE TABLE org_members (
    org_id     BIGINT NOT NULL,
    user_id    TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT org_members_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX idx_org_members_user_id ON org_members(user_id);

CREATE TABLE tasks (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    parent        BIGINT NOT NULL DEFAULT 0,
    runner        TEXT NOT NULL,
    workspace     TEXT NOT NULL,
    instructions  TEXT NOT NULL,
    status        INTEGER NOT NULL,
    command       INTEGER NOT NULL DEFAULT 0,
    version       BIGINT NOT NULL DEFAULT 0,
    org_id        BIGINT NOT NULL,
    archived      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_tasks_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE
);
CREATE INDEX idx_tasks_org_id ON tasks(org_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_parent ON tasks(parent);
CREATE INDEX idx_tasks_runner_status ON tasks(runner, status);
CREATE INDEX idx_tasks_archived ON tasks(archived);

CREATE TABLE logs (
    id         BIGSERIAL PRIMARY KEY,
    task_id    BIGINT NOT NULL,
    type       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT logs_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX idx_logs_task_id ON logs(task_id);

CREATE TABLE task_links (
    id         BIGSERIAL PRIMARY KEY,
    task_id    BIGINT NOT NULL,
    relevance  TEXT NOT NULL,
    url        TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    subscribe  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT task_links_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX idx_task_links_task_id ON task_links(task_id);
CREATE INDEX idx_task_links_url ON task_links(url);

CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    description TEXT NOT NULL,
    data        TEXT NOT NULL,
    url         TEXT NOT NULL DEFAULT '',
    org_id      BIGINT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_events_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE
);
CREATE INDEX idx_events_url ON events(url);
CREATE INDEX idx_events_org_id ON events(org_id);

CREATE TABLE event_tasks (
    event_id BIGINT NOT NULL,
    task_id  BIGINT NOT NULL,
    PRIMARY KEY (event_id, task_id),
    CONSTRAINT event_tasks_event_id_fkey FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE,
    CONSTRAINT event_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX idx_event_tasks_task_id ON event_tasks(task_id);

CREATE TABLE workspaces (
    id          BIGSERIAL PRIMARY KEY,
    runner_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    org_id      BIGINT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_workspaces_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE
);
CREATE INDEX idx_workspaces_runner_id ON workspaces(runner_id);
CREATE INDEX idx_workspaces_org_id ON workspaces(org_id);

CREATE TABLE keys (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    token_hash TEXT NOT NULL,
    org_id     BIGINT NOT NULL,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_keys_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE
);
CREATE INDEX idx_keys_org_id ON keys(org_id);
CREATE UNIQUE INDEX idx_keys_token_hash ON keys(token_hash);

