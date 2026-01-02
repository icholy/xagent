package store

import (
	"database/sql"
	"time"
)

type Link struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Type      string    `json:"type"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

type LinkRepository struct {
	db *sql.DB
}

func NewLinkRepository(db *sql.DB) *LinkRepository {
	return &LinkRepository{db: db}
}

func (r *LinkRepository) Create(link *Link) error {
	result, err := r.db.Exec(`
		INSERT INTO task_links (task_id, type, url, title, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, link.TaskID, link.Type, link.URL, link.Title, link.CreatedAt)
	if err != nil {
		return err
	}
	link.ID, _ = result.LastInsertId()
	return nil
}

func (r *LinkRepository) ListByTask(taskID string) ([]*Link, error) {
	rows, err := r.db.Query(`
		SELECT id, task_id, type, url, title, created_at
		FROM task_links WHERE task_id = ? ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []*Link
	for rows.Next() {
		var link Link
		var title sql.NullString
		if err := rows.Scan(&link.ID, &link.TaskID, &link.Type, &link.URL, &title, &link.CreatedAt); err != nil {
			return nil, err
		}
		link.Title = title.String
		links = append(links, &link)
	}
	return links, rows.Err()
}

func (r *LinkRepository) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM task_links WHERE id = ?`, id)
	return err
}

func (r *LinkRepository) FindByURL(url string) ([]*Link, error) {
	rows, err := r.db.Query(`
		SELECT id, task_id, type, url, title, created_at
		FROM task_links WHERE url = ? ORDER BY created_at DESC
	`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []*Link
	for rows.Next() {
		var link Link
		var title sql.NullString
		if err := rows.Scan(&link.ID, &link.TaskID, &link.Type, &link.URL, &title, &link.CreatedAt); err != nil {
			return nil, err
		}
		link.Title = title.String
		links = append(links, &link)
	}
	return links, rows.Err()
}
