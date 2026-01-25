package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
)

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) exec(tx *sql.Tx) Executor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *EventRepository) Create(ctx context.Context, tx *sql.Tx, event *model.Event) error {
	result, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO events (description, data, url, owner, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, event.Description, event.Data, event.URL, event.Owner, time.Now())
	if err != nil {
		return err
	}
	event.ID, _ = result.LastInsertId()
	return nil
}

func (r *EventRepository) Get(ctx context.Context, tx *sql.Tx, id int64, owner string) (*model.Event, error) {
	var event model.Event
	var url sql.NullString
	err := r.exec(tx).QueryRowContext(ctx, `
		SELECT id, description, data, url, owner, created_at
		FROM events WHERE id = ? AND owner = ?
	`, id, owner).Scan(&event.ID, &event.Description, &event.Data, &url, &event.Owner, &event.CreatedAt)
	if err != nil {
		return nil, err
	}
	event.URL = url.String
	return &event, nil
}

func (r *EventRepository) HasEvent(ctx context.Context, tx *sql.Tx, id int64, owner string) (bool, error) {
	var exists bool
	err := r.exec(tx).QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM events WHERE id = ? AND owner = ?)
	`, id, owner).Scan(&exists)
	return exists, err
}

func (r *EventRepository) List(ctx context.Context, tx *sql.Tx, limit int, owner string) ([]*model.Event, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT id, description, data, url, owner, created_at
		FROM events WHERE owner = ? ORDER BY created_at DESC
		LIMIT ?
	`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) FindByURL(ctx context.Context, tx *sql.Tx, url string) ([]*model.Event, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT id, description, data, url, created_at
		FROM events WHERE url = ? ORDER BY created_at DESC
	`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) Delete(ctx context.Context, tx *sql.Tx, id int64, owner string) error {
	return WithTx(ctx, r.db, tx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM event_tasks WHERE event_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id = ? AND owner = ?`, id, owner); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (r *EventRepository) AddTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO event_tasks (event_id, task_id) VALUES (?, ?)
	`, eventID, taskID)
	return err
}

func (r *EventRepository) RemoveTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		DELETE FROM event_tasks WHERE event_id = ? AND task_id = ?
	`, eventID, taskID)
	return err
}

func (r *EventRepository) ListTasks(ctx context.Context, tx *sql.Tx, eventID int64, owner string) ([]int64, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT et.task_id
		FROM event_tasks et
		JOIN tasks t ON et.task_id = t.id
		WHERE et.event_id = ? AND t.owner = ?
	`, eventID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []int64
	for rows.Next() {
		var taskID int64
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		tasks = append(tasks, taskID)
	}
	return tasks, rows.Err()
}

func (r *EventRepository) ListByTask(ctx context.Context, tx *sql.Tx, taskID int64, owner string) ([]*model.Event, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT e.id, e.description, e.data, e.url, e.owner, e.created_at
		FROM events e
		JOIN event_tasks et ON e.id = et.event_id
		JOIN tasks t ON et.task_id = t.id
		WHERE et.task_id = ? AND t.owner = ?
		ORDER BY e.created_at DESC
	`, taskID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) scanEvents(rows *sql.Rows) ([]*model.Event, error) {
	var events []*model.Event
	for rows.Next() {
		var event model.Event
		var url sql.NullString
		if err := rows.Scan(&event.ID, &event.Description, &event.Data, &url, &event.Owner, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.URL = url.String
		events = append(events, &event)
	}
	return events, rows.Err()
}
