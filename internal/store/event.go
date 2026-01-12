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

func (r *EventRepository) txdb(tx *sql.Tx) *TxDB {
	return NewTxDB(r.db, tx)
}

func (r *EventRepository) Create(ctx context.Context, tx *sql.Tx, event *model.Event) error {
	result, err := r.txdb(tx).ExecContext(ctx, `
		INSERT INTO events (description, data, url, created_at)
		VALUES (?, ?, ?, ?)
	`, event.Description, event.Data, event.URL, time.Now())
	if err != nil {
		return err
	}
	event.ID, _ = result.LastInsertId()
	return nil
}

func (r *EventRepository) Get(ctx context.Context, tx *sql.Tx, id int64) (*model.Event, error) {
	var event model.Event
	var url sql.NullString
	err := r.txdb(tx).QueryRowContext(ctx, `
		SELECT id, description, data, url, created_at
		FROM events WHERE id = ?
	`, id).Scan(&event.ID, &event.Description, &event.Data, &url, &event.CreatedAt)
	if err != nil {
		return nil, err
	}
	event.URL = url.String
	return &event, nil
}

func (r *EventRepository) List(ctx context.Context, tx *sql.Tx, limit int) ([]*model.Event, error) {
	rows, err := r.txdb(tx).QueryContext(ctx, `
		SELECT id, description, data, url, created_at
		FROM events ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) FindByURL(ctx context.Context, tx *sql.Tx, url string) ([]*model.Event, error) {
	rows, err := r.txdb(tx).QueryContext(ctx, `
		SELECT id, description, data, url, created_at
		FROM events WHERE url = ? ORDER BY created_at DESC
	`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) Delete(ctx context.Context, tx *sql.Tx, id int64) error {
	// If no tx was provided, create one
	if tx == nil {
		return WithTx(ctx, r.db, func(innerTx *sql.Tx) error {
			return r.Delete(ctx, innerTx, id)
		})
	}

	db := r.txdb(tx)
	if _, err := db.ExecContext(ctx, `DELETE FROM event_tasks WHERE event_id = ?`, id); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id); err != nil {
		return err
	}
	return nil
}

func (r *EventRepository) AddTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	_, err := r.txdb(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO event_tasks (event_id, task_id) VALUES (?, ?)
	`, eventID, taskID)
	return err
}

func (r *EventRepository) RemoveTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	_, err := r.txdb(tx).ExecContext(ctx, `
		DELETE FROM event_tasks WHERE event_id = ? AND task_id = ?
	`, eventID, taskID)
	return err
}

func (r *EventRepository) ListTasks(ctx context.Context, tx *sql.Tx, eventID int64) ([]int64, error) {
	rows, err := r.txdb(tx).QueryContext(ctx, `
		SELECT task_id FROM event_tasks WHERE event_id = ?
	`, eventID)
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

func (r *EventRepository) ListByTask(ctx context.Context, tx *sql.Tx, taskID int64) ([]*model.Event, error) {
	rows, err := r.txdb(tx).QueryContext(ctx, `
		SELECT e.id, e.description, e.data, e.url, e.created_at
		FROM events e
		JOIN event_tasks et ON e.id = et.event_id
		WHERE et.task_id = ?
		ORDER BY e.created_at DESC
	`, taskID)
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
		if err := rows.Scan(&event.ID, &event.Description, &event.Data, &url, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.URL = url.String
		events = append(events, &event)
	}
	return events, rows.Err()
}
