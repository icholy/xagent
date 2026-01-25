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
	id, err := s.queries(tx).CreateTask(ctx, sqlc.CreateTaskParams{
		Name:         task.Name,
		Parent:       task.Parent,
		Runner:       task.Runner,
		Workspace:    task.Workspace,
		Instructions: string(instructions),
		Status:       string(task.Status),
		Command:      string(task.Command),
		Version:      task.Version,
		Owner:        task.Owner,
		CreatedAt:    sql.NullTime{Time: now, Valid: true},
		UpdatedAt:    sql.NullTime{Time: now, Valid: true},
	})
	if err != nil {
		return err
	}

	task.ID = id
	task.CreatedAt = now
	task.UpdatedAt = now
	return nil
}

func (s *Store) GetTask(ctx context.Context, tx *sql.Tx, id int64, owner string) (*model.Task, error) {
	row, err := s.queries(tx).GetTask(ctx, sqlc.GetTaskParams{
		ID:    id,
		Owner: owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelTask(row)
}

func (s *Store) HasTask(ctx context.Context, tx *sql.Tx, id int64, owner string) (bool, error) {
	exists, err := s.queries(tx).HasTask(ctx, sqlc.HasTaskParams{
		ID:    id,
		Owner: owner,
	})
	return exists != 0, err
}

func (s *Store) ListTasks(ctx context.Context, tx *sql.Tx, owner string) ([]*model.Task, error) {
	rows, err := s.queries(tx).ListTasks(ctx, owner)
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (s *Store) ListTaskChildren(ctx context.Context, tx *sql.Tx, parentID int64, owner string) ([]*model.Task, error) {
	rows, err := s.queries(tx).ListTaskChildren(ctx, sqlc.ListTaskChildrenParams{
		Parent: parentID,
		Owner:  owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (s *Store) ListTasksForRunner(ctx context.Context, tx *sql.Tx, runner string, owner string) ([]*model.Task, error) {
	rows, err := s.queries(tx).ListTasksForRunner(ctx, sqlc.ListTasksForRunnerParams{
		Runner: runner,
		Owner:  owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (s *Store) ListTasksByEvent(ctx context.Context, tx *sql.Tx, eventID int64) ([]*model.Task, error) {
	rows, err := s.queries(tx).ListTasksByEvent(ctx, eventID)
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
	return s.queries(tx).UpdateTask(ctx, sqlc.UpdateTaskParams{
		Name:         task.Name,
		Parent:       task.Parent,
		Runner:       task.Runner,
		Workspace:    task.Workspace,
		Instructions: string(instructions),
		Status:       string(task.Status),
		Command:      string(task.Command),
		Version:      task.Version,
		UpdatedAt:    sql.NullTime{Time: task.UpdatedAt, Valid: true},
		ID:           task.ID,
		Owner:        task.Owner,
	})
}

func (s *Store) DeleteTask(ctx context.Context, tx *sql.Tx, id int64, owner string) error {
	return s.queries(tx).DeleteTask(ctx, sqlc.DeleteTaskParams{
		ID:    id,
		Owner: owner,
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
		Owner:        row.Owner,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
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
