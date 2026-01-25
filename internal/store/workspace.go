package store

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
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

// List returns all workspaces for the given owner, sorted alphabetically by name.
func (r *WorkspaceRepository) List(ctx context.Context, tx *sql.Tx, owner string) ([]*model.Workspace, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT id, runner_id, name, owner, created_at
		FROM workspaces WHERE owner = ? ORDER BY name ASC
	`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workspaces []*model.Workspace
	for rows.Next() {
		ws, err := r.scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}

func (r *WorkspaceRepository) scanWorkspace(rows *sql.Rows) (*model.Workspace, error) {
	var ws model.Workspace
	err := rows.Scan(&ws.ID, &ws.RunnerID, &ws.Name, &ws.Owner, &ws.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}
