package store

import (
	"context"
	"database/sql"
)

type WorkspaceRepository struct {
	db *sql.DB
}

func NewWorkspaceRepository(db *sql.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

func (r *WorkspaceRepository) exec(tx *sql.Tx) Executor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *WorkspaceRepository) WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	return WithTx(ctx, r.db, tx, f)
}

// DeleteByRunner deletes all workspaces for the given runner ID and owner.
func (r *WorkspaceRepository) DeleteByRunner(ctx context.Context, tx *sql.Tx, runnerID, owner string) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM workspaces WHERE runner_id = ? AND owner = ?`, runnerID, owner)
	return err
}

// Create inserts a new workspace for the given runner ID and owner.
func (r *WorkspaceRepository) Create(ctx context.Context, tx *sql.Tx, runnerID, name, owner string) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO workspaces (runner_id, name, owner)
		VALUES (?, ?, ?)
	`, runnerID, name, owner)
	return err
}

// List returns all unique workspace names for the given owner, sorted alphabetically.
func (r *WorkspaceRepository) List(ctx context.Context, tx *sql.Tx, owner string) ([]string, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT DISTINCT name FROM workspaces WHERE owner = ? ORDER BY name ASC
	`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
