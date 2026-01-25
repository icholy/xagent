package store

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateLink(ctx context.Context, tx *sql.Tx, link *model.Link) error {
	id, err := s.q(tx).CreateLink(ctx, sqlc.CreateLinkParams{
		TaskID:    link.TaskID,
		Relevance: link.Relevance,
		Url:       link.URL,
		Title:     link.Title,
		Notify:    link.Notify,
		CreatedAt: link.CreatedAt,
	})
	if err != nil {
		return err
	}
	link.ID = id
	return nil
}

func (s *Store) ListLinksByTask(ctx context.Context, tx *sql.Tx, taskID int64, owner string) ([]*model.Link, error) {
	rows, err := s.q(tx).ListLinksByTask(ctx, sqlc.ListLinksByTaskParams{
		TaskID: taskID,
		Owner:  owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelLinks(rows), nil
}

func (s *Store) DeleteLink(ctx context.Context, tx *sql.Tx, id int64) error {
	return s.q(tx).DeleteLink(ctx, id)
}

func (s *Store) FindLinksByURL(ctx context.Context, tx *sql.Tx, url string, owner string) ([]*model.Link, error) {
	rows, err := s.q(tx).FindLinksByURL(ctx, sqlc.FindLinksByURLParams{
		Url:   url,
		Owner: owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelLinks(rows), nil
}

func toModelLinks(rows []sqlc.TaskLink) []*model.Link {
	links := make([]*model.Link, len(rows))
	for i, row := range rows {
		links[i] = &model.Link{
			ID:        row.ID,
			TaskID:    row.TaskID,
			Relevance: row.Relevance,
			URL:       row.Url,
			Title:     row.Title,
			Notify:    row.Notify,
			CreatedAt: row.CreatedAt,
		}
	}
	return links
}
