package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

type Event struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Data        string    `json:"data"`
	URL         string    `json:"url,omitempty"`
	Tasks       []string  `json:"tasks,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) Create(event *Event) error {
	tasks, err := json.Marshal(event.Tasks)
	if err != nil {
		return err
	}
	result, err := r.db.Exec(`
		INSERT INTO events (description, data, url, tasks, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, event.Description, event.Data, event.URL, string(tasks), time.Now())
	if err != nil {
		return err
	}
	event.ID, _ = result.LastInsertId()
	return nil
}

func (r *EventRepository) Get(id int64) (*Event, error) {
	var event Event
	var url sql.NullString
	var tasks string
	err := r.db.QueryRow(`
		SELECT id, description, data, url, tasks, created_at
		FROM events WHERE id = ?
	`, id).Scan(&event.ID, &event.Description, &event.Data, &url, &tasks, &event.CreatedAt)
	if err != nil {
		return nil, err
	}
	event.URL = url.String
	if err := json.Unmarshal([]byte(tasks), &event.Tasks); err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *EventRepository) List() ([]*Event, error) {
	rows, err := r.db.Query(`
		SELECT id, description, data, url, tasks, created_at
		FROM events ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) FindByURL(url string) ([]*Event, error) {
	rows, err := r.db.Query(`
		SELECT id, description, data, url, tasks, created_at
		FROM events WHERE url = ? ORDER BY created_at DESC
	`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanEvents(rows)
}

func (r *EventRepository) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM events WHERE id = ?`, id)
	return err
}

func (r *EventRepository) Update(id int64, update EventUpdate) error {
	if update.Tasks != nil {
		tasks, err := json.Marshal(update.Tasks)
		if err != nil {
			return err
		}
		_, err = r.db.Exec(`UPDATE events SET tasks = ? WHERE id = ?`, string(tasks), id)
		if err != nil {
			return err
		}
	}
	return nil
}

type EventUpdate struct {
	Tasks []string
}

func (r *EventRepository) scanEvents(rows *sql.Rows) ([]*Event, error) {
	var events []*Event
	for rows.Next() {
		var event Event
		var url sql.NullString
		var tasks string
		if err := rows.Scan(&event.ID, &event.Description, &event.Data, &url, &tasks, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.URL = url.String
		if err := json.Unmarshal([]byte(tasks), &event.Tasks); err != nil {
			return nil, err
		}
		events = append(events, &event)
	}
	return events, rows.Err()
}
