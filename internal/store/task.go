package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateTask(ctx context.Context, tx *sql.Tx, task *model.Task) error {
	instructions, err := json.Marshal(task.Instructions)
	if err != nil {
		return err
	}
	now := time.Now()
	id, err := s.q(tx).CreateTask(ctx, sqlc.CreateTaskParams{
		Name:         task.Name,
		Parent:       task.Parent,
		Runner:       task.Runner,
		Workspace:    task.Workspace,
		Instructions: string(instructions),
		Status:       int32(task.Status),
		Command:      int32(task.Command),
		Version:      task.Version,
		OrgID:        task.OrgID,
		CreatedAt:    now,
		UpdatedAt:    now,
		Archived:     task.Archived,
	})
	if err != nil {
		return err
	}
	task.ID = id
	task.CreatedAt = now
	task.UpdatedAt = now
	return nil
}

func (s *Store) GetTask(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (*model.Task, error) {
	row, err := s.q(tx).GetTask(ctx, sqlc.GetTaskParams{
		ID:    id,
		OrgID: orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelTask(row)
}

func (s *Store) GetTaskForUpdate(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (*model.Task, error) {
	row, err := s.q(tx).GetTaskForUpdate(ctx, sqlc.GetTaskForUpdateParams{
		ID:    id,
		OrgID: orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelTask(row)
}

func (s *Store) HasTask(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (bool, error) {
	return s.q(tx).HasTask(ctx, sqlc.HasTaskParams{
		ID:    id,
		OrgID: orgID,
	})
}

func (s *Store) ListTasks(ctx context.Context, tx *sql.Tx, orgID int64) ([]*model.Task, error) {
	rows, err := s.q(tx).ListTasks(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (s *Store) ListTaskChildren(ctx context.Context, tx *sql.Tx, parentID int64, orgID int64) ([]*model.Task, error) {
	rows, err := s.q(tx).ListTaskChildren(ctx, sqlc.ListTaskChildrenParams{
		Parent: parentID,
		OrgID:  orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (s *Store) ListTasksForRunner(ctx context.Context, tx *sql.Tx, runner string, orgID int64) ([]*model.Task, error) {
	rows, err := s.q(tx).ListTasksForRunner(ctx, sqlc.ListTasksForRunnerParams{
		Runner: runner,
		OrgID:  orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (s *Store) ListTasksByEvent(ctx context.Context, tx *sql.Tx, eventID int64) ([]*model.Task, error) {
	rows, err := s.q(tx).ListTasksByEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (s *Store) UpdateTask(ctx context.Context, tx *sql.Tx, task *model.Task) error {
	instructions, err := json.Marshal(task.Instructions)
	if err != nil {
		return err
	}
	task.UpdatedAt = time.Now()
	return s.q(tx).UpdateTask(ctx, sqlc.UpdateTaskParams{
		Name:         task.Name,
		Parent:       task.Parent,
		Runner:       task.Runner,
		Workspace:    task.Workspace,
		Instructions: string(instructions),
		Status:       int32(task.Status),
		Command:      int32(task.Command),
		Version:      task.Version,
		UpdatedAt:    task.UpdatedAt,
		Archived:     task.Archived,
		ID:           task.ID,
		OrgID:        task.OrgID,
	})
}

func (s *Store) DeleteTask(ctx context.Context, tx *sql.Tx, id int64, orgID int64) error {
	return s.q(tx).DeleteTask(ctx, sqlc.DeleteTaskParams{
		ID:    id,
		OrgID: orgID,
	})
}

func toModelTask(row sqlc.Task) (*model.Task, error) {
	var instructions []model.Instruction
	if err := json.Unmarshal([]byte(row.Instructions), &instructions); err != nil {
		return nil, err
	}
	return &model.Task{
		ID:           row.ID,
		Name:         row.Name,
		Parent:       row.Parent,
		Runner:       row.Runner,
		Workspace:    row.Workspace,
		Instructions: instructions,
		Status:       model.TaskStatus(row.Status),
		Command:      model.TaskCommand(row.Command),
		Version:      row.Version,
		OrgID:        row.OrgID,
		Archived:     row.Archived,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

func toModelTasks(rows []sqlc.Task) ([]*model.Task, error) {
	tasks := make([]*model.Task, len(rows))
	for i, row := range rows {
		task, err := toModelTask(row)
		if err != nil {
			return nil, err
		}
		tasks[i] = task
	}
	return tasks, nil
}
