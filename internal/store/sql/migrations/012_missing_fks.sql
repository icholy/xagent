-- +goose Up

ALTER TABLE users ADD CONSTRAINT fk_users_default_org_id FOREIGN KEY (default_org_id) REFERENCES orgs(id);
ALTER TABLE orgs ADD CONSTRAINT fk_orgs_owner FOREIGN KEY (owner) REFERENCES users(id);

-- +goose Down

ALTER TABLE orgs DROP CONSTRAINT fk_orgs_owner;
ALTER TABLE users DROP CONSTRAINT fk_users_default_org_id;
