-- +goose Up

ALTER TABLE tasks ADD CONSTRAINT fk_tasks_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);
ALTER TABLE events ADD CONSTRAINT fk_events_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);
ALTER TABLE workspaces ADD CONSTRAINT fk_workspaces_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);
ALTER TABLE keys ADD CONSTRAINT fk_keys_org_id FOREIGN KEY (org_id) REFERENCES orgs(id);
ALTER TABLE org_members DROP CONSTRAINT org_members_org_id_fkey;
ALTER TABLE org_members ADD CONSTRAINT org_members_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;

-- +goose Down

ALTER TABLE org_members DROP CONSTRAINT org_members_org_id_fkey;
ALTER TABLE org_members ADD CONSTRAINT org_members_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id);
ALTER TABLE keys DROP CONSTRAINT fk_keys_org_id;
ALTER TABLE workspaces DROP CONSTRAINT fk_workspaces_org_id;
ALTER TABLE events DROP CONSTRAINT fk_events_org_id;
ALTER TABLE tasks DROP CONSTRAINT fk_tasks_org_id;
