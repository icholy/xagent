package store

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

type WorkspaceRepository struct {
	db *sql.DB
}

func NewWorkspaceRepository(db *sql.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

func (r *WorkspaceRepository) queries(tx *sql.Tx) *sqlc.Queries {
	if tx != nil {
		return sqlc.New(tx)
	}
	return sqlc.New(r.db)
}

func (r *WorkspaceRepository) WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	return WithTx(ctx, r.db, tx, f)
}

// DeleteByRunner deletes all workspaces for the given runner ID and owner.
func (r *WorkspaceRepository) DeleteByRunner(ctx context.Context, tx *sql.Tx, runnerID, owner string) error {
	return r.queries(tx).DeleteWorkspacesByRunner(ctx, sqlc.DeleteWorkspacesByRunnerParams{
		RunnerID: runnerID,
		Owner:    owner,
	})
}

// Create inserts a new workspace for the given runner ID and owner.
func (r *WorkspaceRepository) Create(ctx context.Context, tx *sql.Tx, runnerID, name, owner string) error {
	return r.queries(tx).CreateWorkspace(ctx, sqlc.CreateWorkspaceParams{
		RunnerID: runnerID,
		Name:     name,
		Owner:    owner,
	})
}

// List returns all workspaces for the given owner, sorted alphabetically by name.
func (r *WorkspaceRepository) List(ctx context.Context, tx *sql.Tx, owner string) ([]*model.Workspace, error) {
	rows, err := r.queries(tx).ListWorkspacesByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	workspaces := make([]*model.Workspace, len(rows))
	for i, row := range rows {
		workspaces[i] = &model.Workspace{
			ID:        row.ID,
			RunnerID:  row.RunnerID,
			Name:      row.Name,
			Owner:     row.Owner,
			UpdatedAt: row.UpdatedAt.Time,
		}
	}
	return workspaces, nil
}

// Clear deletes all workspaces for the given owner.
func (r *WorkspaceRepository) Clear(ctx context.Context, tx *sql.Tx, owner string) error {
	return r.queries(tx).ClearWorkspacesByOwner(ctx, owner)
}
