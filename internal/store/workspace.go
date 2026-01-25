package store

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) DeleteWorkspacesByRunner(ctx context.Context, tx *sql.Tx, runnerID, owner string) error {
	return s.queries(tx).DeleteWorkspacesByRunner(ctx, sqlc.DeleteWorkspacesByRunnerParams{
		RunnerID: runnerID,
		Owner:    owner,
	})
}

func (s *Store) CreateWorkspace(ctx context.Context, tx *sql.Tx, runnerID, name, owner string) error {
	return s.queries(tx).CreateWorkspace(ctx, sqlc.CreateWorkspaceParams{
		RunnerID: runnerID,
		Name:     name,
		Owner:    owner,
	})
}

func (s *Store) ListWorkspaces(ctx context.Context, tx *sql.Tx, owner string) ([]*model.Workspace, error) {
	rows, err := s.queries(tx).ListWorkspacesByOwner(ctx, owner)
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

func (s *Store) ClearWorkspaces(ctx context.Context, tx *sql.Tx, owner string) error {
	return s.queries(tx).ClearWorkspacesByOwner(ctx, owner)
}
