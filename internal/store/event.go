package store

import (
	"database/sql"
	"time"
)

type Event struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Data        string    `json:"data"`
	URL         string    `json:"url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) Create(event *Event) error {
	result, err := r.db.Exec(`
		INSERT INTO events (description, data, url, created_at)
		VALUES (?, ?, ?, ?)
	`, event.Description, event.Data, event.URL, time.Now())
	if err != nil {
		return err
	}
	event.ID, _ = result.LastInsertId()
	return nil
}

func (r *EventRepository) Get(id int64) (*Event, error) {
	var event Event
	var url sql.NullString
	err := r.db.QueryRow(`
		SELECT id, description, data, url, created_at
		FROM events WHERE id = ?
	`, id).Scan(&event.ID, &event.Description, &event.Data, &url, &event.CreatedAt)
	if err != nil {
		return nil, err
	}
	event.URL = url.String
	return &event, nil
}

func (r *EventRepository) List(limit int) ([]*Event, error) {
	rows, err := r.db.Query(`
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

func (r *EventRepository) FindByURL(url string) ([]*Event, error) {
	rows, err := r.db.Query(`
		SELECT id, description, data, url, created_at
		FROM events WHERE url = ? ORDER BY created_at DESC
	`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) Delete(id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM event_tasks WHERE event_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM events WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *EventRepository) AddTask(eventID int64, taskID int64) error {
	_, err := r.db.Exec(`
		INSERT OR IGNORE INTO event_tasks (event_id, task_id) VALUES (?, ?)
	`, eventID, taskID)
	return err
}

func (r *EventRepository) RemoveTask(eventID int64, taskID int64) error {
	_, err := r.db.Exec(`
		DELETE FROM event_tasks WHERE event_id = ? AND task_id = ?
	`, eventID, taskID)
	return err
}

func (r *EventRepository) ListTasks(eventID int64) ([]int64, error) {
	rows, err := r.db.Query(`
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

func (r *EventRepository) ListByTask(taskID int64) ([]*Event, error) {
	rows, err := r.db.Query(`
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

func (r *EventRepository) scanEvents(rows *sql.Rows) ([]*Event, error) {
	var events []*Event
	for rows.Next() {
		var event Event
		var url sql.NullString
		if err := rows.Scan(&event.ID, &event.Description, &event.Data, &url, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.URL = url.String
		events = append(events, &event)
	}
	return events, rows.Err()
}
