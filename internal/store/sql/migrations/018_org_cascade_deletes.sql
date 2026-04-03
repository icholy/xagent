-- +goose Up

-- Add ON DELETE CASCADE to all foreign keys referencing orgs(id)
-- so that deleting an org removes all associated data.

-- tasks.org_id
ALTER TABLE tasks DROP CONSTRAINT fk_tasks_org_id;
ALTER TABLE tasks ADD CONSTRAINT fk_tasks_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;

-- events.org_id
ALTER TABLE events DROP CONSTRAINT fk_events_org_id;
ALTER TABLE events ADD CONSTRAINT fk_events_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;

-- workspaces.org_id
ALTER TABLE workspaces DROP CONSTRAINT fk_workspaces_org_id;
ALTER TABLE workspaces ADD CONSTRAINT fk_workspaces_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;

-- keys.org_id
ALTER TABLE keys DROP CONSTRAINT fk_keys_org_id;
ALTER TABLE keys ADD CONSTRAINT fk_keys_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;

-- users.default_org_id (SET NULL instead of CASCADE - don't delete users)
ALTER TABLE users DROP CONSTRAINT fk_users_default_org_id;
ALTER TABLE users ADD CONSTRAINT fk_users_default_org_id FOREIGN KEY (default_org_id) REFERENCES orgs(id) ON DELETE SET NULL;

-- Add ON DELETE CASCADE to child tables of tasks and events
-- so the cascade chain works end-to-end.

-- logs.task_id
ALTER TABLE logs DROP CONSTRAINT logs_task_id_fkey;
ALTER TABLE logs ADD CONSTRAINT logs_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;

-- task_links.task_id
ALTER TABLE task_links DROP CONSTRAINT task_links_task_id_fkey;
ALTER TABLE task_links ADD CONSTRAINT task_links_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;

-- event_tasks.task_id
ALTER TABLE event_tasks DROP CONSTRAINT event_tasks_task_id_fkey;
ALTER TABLE event_tasks ADD CONSTRAINT event_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;

-- event_tasks.event_id
ALTER TABLE event_tasks DROP CONSTRAINT event_tasks_event_id_fkey;
ALTER TABLE event_tasks ADD CONSTRAINT event_tasks_event_id_fkey FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE;

-- +goose Down

ALTER TABLE event_tasks DROP CONSTRAINT event_tasks_event_id_fkey;
ALTER TABLE event_tasks ADD CONSTRAINT event_tasks_event_id_fkey FOREIGN KEY (event_id) REFERENCES events(id);

ALTER TABLE event_tasks DROP CONSTRAINT event_tasks_task_id_fkey;
ALTER TABLE event_tasks ADD CONSTRAINT event_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id);

ALTER TABLE task_links DROP CONSTRAINT task_links_task_id_fkey;
ALTER TABLE task_links ADD CONSTRAINT task_links_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id);

ALTER TABLE logs DROP CONSTRAINT logs_task_id_fkey;
ALTER TABLE logs ADD CONSTRAINT logs_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id);

ALTER TABLE users DROP CONSTRAINT fk_users_default_org_id;
ALTER TABLE users ADD CONSTRAINT fk_users_default_org_id FOREIGN KEY (default_org_id) REFERENCES orgs(id);

ALTER TABLE keys DROP CONSTRAINT fk_keys_org_id;
ALTER TABLE keys ADD CONSTRAINT fk_keys_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);

ALTER TABLE workspaces DROP CONSTRAINT fk_workspaces_org_id;
ALTER TABLE workspaces ADD CONSTRAINT fk_workspaces_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);

ALTER TABLE events DROP CONSTRAINT fk_events_org_id;
ALTER TABLE events ADD CONSTRAINT fk_events_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);

ALTER TABLE tasks DROP CONSTRAINT fk_tasks_org_id;
ALTER TABLE tasks ADD CONSTRAINT fk_tasks_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);
