package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
	TaskStatusArchived  TaskStatus = "archived"
)

type Instruction struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

type Task struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Workspace    string        `json:"workspace"`
	Instructions []Instruction `json:"instructions"`
	Status       TaskStatus    `json:"status"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type TaskRepository struct {
	db *sql.DB
}

func NewTaskRepository(db *sql.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) Create(task *Task) error {
	instructions, err := json.Marshal(task.Instructions)
	if err != nil {
		return err
	}

	now := time.Now()
	_, err = r.db.Exec(`
		INSERT INTO tasks (id, name, workspace, prompts, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, task.ID, task.Name, task.Workspace, instructions, task.Status, now, now)
	return err
}

func (r *TaskRepository) Get(id string) (*Task, error) {
	row := r.db.QueryRow(`
		SELECT id, name, workspace, prompts, status, created_at, updated_at
		FROM tasks WHERE id = ?
	`, id)
	return r.scanTask(row)
}

func (r *TaskRepository) List() ([]*Task, error) {
	rows, err := r.db.Query(`
		SELECT id, name, workspace, prompts, status, created_at, updated_at
		FROM tasks WHERE status != 'archived' ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanTasks(rows)
}

func (r *TaskRepository) ListByStatuses(statuses []TaskStatus) ([]*Task, error) {
	if len(statuses) == 0 {
		return r.List()
	}
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, s := range statuses {
		placeholders[i] = "?"
		args[i] = s
	}
	query := fmt.Sprintf(`
		SELECT id, name, workspace, prompts, status, created_at, updated_at
		FROM tasks WHERE status IN (%s) ORDER BY created_at DESC
	`, strings.Join(placeholders, ","))
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanTasks(rows)
}

type TaskUpdate struct {
	Name            string
	Status          TaskStatus
	AddInstructions []Instruction
}

func (r *TaskRepository) Update(id string, update TaskUpdate) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Add instructions if provided
	if len(update.AddInstructions) > 0 {
		row := tx.QueryRow(`SELECT prompts FROM tasks WHERE id = ?`, id)

		var existing []byte
		if err := row.Scan(&existing); err != nil {
			return err
		}

		var all []Instruction
		if err := json.Unmarshal(existing, &all); err != nil {
			return err
		}

		all = append(all, update.AddInstructions...)
		data, err := json.Marshal(all)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`
			UPDATE tasks SET prompts = ?, updated_at = ? WHERE id = ?
		`, data, time.Now(), id)
		if err != nil {
			return err
		}
	}

	// Update name if provided
	if update.Name != "" {
		_, err = tx.Exec(`
			UPDATE tasks SET name = ?, updated_at = ? WHERE id = ?
		`, update.Name, time.Now(), id)
		if err != nil {
			return err
		}
	}

	// Update status if provided
	if update.Status != "" {
		_, err = tx.Exec(`
			UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?
		`, update.Status, time.Now(), id)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *TaskRepository) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return err
}

func (r *TaskRepository) scanTask(row *sql.Row) (*Task, error) {
	var task Task
	var instructions []byte

	err := row.Scan(
		&task.ID,
		&task.Name,
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

func (r *TaskRepository) scanTasks(rows *sql.Rows) ([]*Task, error) {
	var tasks []*Task
	for rows.Next() {
		var task Task
		var instructions []byte

		err := rows.Scan(
			&task.ID,
			&task.Name,
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
