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

// Register deletes all existing workspaces for the given runner ID and inserts
// the new workspace names. This should be called within a transaction.
func (r *WorkspaceRepository) Register(ctx context.Context, tx *sql.Tx, runnerID string, names []string) error {
	// Delete all existing workspaces for this runner
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM workspaces WHERE runner_id = ?`, runnerID)
	if err != nil {
		return err
	}

	// Insert new workspaces
	for _, name := range names {
		_, err := r.exec(tx).ExecContext(ctx, `
			INSERT INTO workspaces (runner_id, name)
			VALUES (?, ?)
		`, runnerID, name)
		if err != nil {
			return err
		}
	}

	return nil
}

// List returns all unique workspace names across all runners, sorted alphabetically.
func (r *WorkspaceRepository) List(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT DISTINCT name FROM workspaces ORDER BY name ASC
	`)
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
