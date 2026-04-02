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
		Subscribe: link.Subscribe,
		CreatedAt: link.CreatedAt,
	})
	if err != nil {
		return err
	}
	link.ID = id
	return nil
}

func (s *Store) ListLinksByTask(ctx context.Context, tx *sql.Tx, taskID int64, orgID int64) ([]*model.Link, error) {
	rows, err := s.q(tx).ListLinksByTask(ctx, sqlc.ListLinksByTaskParams{
		TaskID: taskID,
		OrgID:  orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelLinks(rows), nil
}

func (s *Store) DeleteLink(ctx context.Context, tx *sql.Tx, id int64) error {
	return s.q(tx).DeleteLink(ctx, id)
}

func (s *Store) FindLinksByURL(ctx context.Context, tx *sql.Tx, url string, orgID int64) ([]*model.Link, error) {
	rows, err := s.q(tx).FindLinksByURL(ctx, sqlc.FindLinksByURLParams{
		Url:   url,
		OrgID: orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelLinks(rows), nil
}

// LinkWithOrg pairs a Link with its task's org ID.
type LinkWithOrg struct {
	Link  *model.Link
	OrgID int64
}

func (s *Store) FindSubscribedLinksByURLForUser(ctx context.Context, tx *sql.Tx, url string, userID string) ([]LinkWithOrg, error) {
	rows, err := s.q(tx).FindSubscribedLinksByURLForUser(ctx, sqlc.FindSubscribedLinksByURLForUserParams{
		Url:    url,
		UserID: userID,
	})
	if err != nil {
		return nil, err
	}
	result := make([]LinkWithOrg, len(rows))
	for i, row := range rows {
		result[i] = LinkWithOrg{
			Link: &model.Link{
				ID:        row.ID,
				TaskID:    row.TaskID,
				Relevance: row.Relevance,
				URL:       row.Url,
				Title:     row.Title,
				Subscribe: row.Subscribe,
				CreatedAt: row.CreatedAt,
			},
			OrgID: row.OrgID,
		}
	}
	return result, nil
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
			Subscribe: row.Subscribe,
			CreatedAt: row.CreatedAt,
		}
	}
	return links
}
