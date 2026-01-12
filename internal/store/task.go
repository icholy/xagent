package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/icholy/xagent/internal/model"
)

type TaskRepository struct {
	db *sql.DB
}

func NewTaskRepository(db *sql.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) exec(tx *sql.Tx) Executor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *TaskRepository) WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	return WithTx(ctx, r.db, tx, f)
}

func (r *TaskRepository) Create(ctx context.Context, tx *sql.Tx, task *model.Task) error {
	instructions, err := json.Marshal(task.Instructions)
	if err != nil {
		return err
	}

	now := time.Now()
	result, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO tasks (name, parent, workspace, prompts, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, task.Name, task.Parent, task.Workspace, instructions, task.Status, now, now)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	task.ID = id
	task.CreatedAt = now
	task.UpdatedAt = now
	return nil
}

func (r *TaskRepository) Get(ctx context.Context, tx *sql.Tx, id int64) (*model.Task, error) {
	row := r.exec(tx).QueryRowContext(ctx, `
		SELECT id, name, parent, workspace, prompts, status, created_at, updated_at
		FROM tasks WHERE id = ?
	`, id)
	return r.scanTask(row)
}

func (r *TaskRepository) List(ctx context.Context, tx *sql.Tx) ([]*model.Task, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT id, name, parent, workspace, prompts, status, created_at, updated_at
		FROM tasks WHERE status != 'archived' ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanTasks(rows)
}

func (r *TaskRepository) ListByStatuses(ctx context.Context, tx *sql.Tx, statuses []model.TaskStatus) ([]*model.Task, error) {
	if len(statuses) == 0 {
		return r.List(ctx, tx)
	}
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, s := range statuses {
		placeholders[i] = "?"
		args[i] = s
	}
	query := fmt.Sprintf(`
		SELECT id, name, parent, workspace, prompts, status, created_at, updated_at
		FROM tasks WHERE status IN (%s) ORDER BY created_at DESC
	`, strings.Join(placeholders, ","))
	rows, err := r.exec(tx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanTasks(rows)
}

func (r *TaskRepository) ListChildren(ctx context.Context, tx *sql.Tx, parentID int64) ([]*model.Task, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT id, name, parent, workspace, prompts, status, created_at, updated_at
		FROM tasks WHERE parent = ? ORDER BY created_at DESC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanTasks(rows)
}

func (r *TaskRepository) ListByEvent(ctx context.Context, tx *sql.Tx, eventID int64) ([]*model.Task, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT t.id, t.name, t.parent, t.workspace, t.prompts, t.status, t.created_at, t.updated_at
		FROM tasks t
		JOIN event_tasks et ON t.id = et.task_id
		WHERE et.event_id = ?
		ORDER BY t.created_at DESC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanTasks(rows)
}

func (r *TaskRepository) Put(ctx context.Context, tx *sql.Tx, task *model.Task) error {
	instructions, err := json.Marshal(task.Instructions)
	if err != nil {
		return err
	}

	task.UpdatedAt = time.Now()
	_, err = r.exec(tx).ExecContext(ctx, `
		UPDATE tasks SET name = ?, parent = ?, workspace = ?, prompts = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, task.Name, task.Parent, task.Workspace, instructions, task.Status, task.UpdatedAt, task.ID)
	return err
}

func (r *TaskRepository) Delete(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

func (r *TaskRepository) scanTask(row *sql.Row) (*model.Task, error) {
	var task model.Task
	var instructions []byte

	err := row.Scan(
		&task.ID,
		&task.Name,
		&task.Parent,
		&task.Workspace,
		&instructions,
		&task.Status,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(instructions, &task.Instructions); err != nil {
		return nil, err
	}

	return &task, nil
}

func (r *TaskRepository) scanTasks(rows *sql.Rows) ([]*model.Task, error) {
	var tasks []*model.Task
	for rows.Next() {
		var task model.Task
		var instructions []byte

		err := rows.Scan(
			&task.ID,
			&task.Name,
			&task.Parent,
			&task.Workspace,
			&instructions,
			&task.Status,
			&task.CreatedAt,
			&task.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(instructions, &task.Instructions); err != nil {
			return nil, err
		}

		tasks = append(tasks, &task)
	}
	return tasks, rows.Err()
}
