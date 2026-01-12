package store

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
)

type LinkRepository struct {
	db *sql.DB
}

func NewLinkRepository(db *sql.DB) *LinkRepository {
	return &LinkRepository{db: db}
}

func (r *LinkRepository) exec(tx *sql.Tx) Executor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *LinkRepository) Create(ctx context.Context, tx *sql.Tx, link *model.Link) error {
	result, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO task_links (task_id, relevance, url, title, notify, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, link.TaskID, link.Relevance, link.URL, link.Title, link.Notify, link.CreatedAt)
	if err != nil {
		return err
	}
	link.ID, _ = result.LastInsertId()
	return nil
}

func (r *LinkRepository) ListByTask(ctx context.Context, tx *sql.Tx, taskID int64) ([]*model.Link, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT id, task_id, relevance, url, title, notify, created_at
		FROM task_links WHERE task_id = ? ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanLinks(rows)
}

func (r *LinkRepository) Delete(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM task_links WHERE id = ?`, id)
	return err
}

func (r *LinkRepository) FindByURL(ctx context.Context, tx *sql.Tx, url string) ([]*model.Link, error) {
	rows, err := r.exec(tx).QueryContext(ctx, `
		SELECT l.id, l.task_id, l.relevance, l.url, l.title, l.notify, l.created_at
		FROM task_links l
		JOIN tasks t ON l.task_id = t.id
		WHERE l.url = ? AND t.status != 'archived'
		ORDER BY l.created_at DESC
	`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanLinks(rows)
}

func (r *LinkRepository) scanLinks(rows *sql.Rows) ([]*model.Link, error) {
	var links []*model.Link
	for rows.Next() {
		var link model.Link
		var title sql.NullString
		if err := rows.Scan(&link.ID, &link.TaskID, &link.Relevance, &link.URL, &title, &link.Notify, &link.CreatedAt); err != nil {
			return nil, err
		}
		link.Title = title.String
		links = append(links, &link)
	}
	return links, rows.Err()
}
