package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateEvent(ctx context.Context, tx *sql.Tx, event *model.Event) error {
	id, err := s.q(tx).CreateEvent(ctx, sqlc.CreateEventParams{
		TaskID:    event.TaskID,
		OrgID:     event.OrgID,
		Type:      event.Type,
		Wake:      event.Wake,
		Payload:   event.Payload,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	event.ID = id
	return nil
}

func (s *Store) GetEvent(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (*model.Event, error) {
	row, err := s.q(tx).GetEvent(ctx, sqlc.GetEventParams{
		ID:    id,
		OrgID: orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelEvent(row), nil
}

func (s *Store) ListEvents(ctx context.Context, tx *sql.Tx, limit int, orgID int64) ([]*model.Event, error) {
	rows, err := s.q(tx).ListEvents(ctx, sqlc.ListEventsParams{
		OrgID: orgID,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows), nil
}

func (s *Store) DeleteEvent(ctx context.Context, tx *sql.Tx, id int64, orgID int64) error {
	return s.q(tx).DeleteEvent(ctx, sqlc.DeleteEventParams{ID: id, OrgID: orgID})
}

func (s *Store) ListEventsByTask(ctx context.Context, tx *sql.Tx, taskID int64, orgID int64) ([]*model.Event, error) {
	rows, err := s.q(tx).ListEventsByTask(ctx, sqlc.ListEventsByTaskParams{
		TaskID: taskID,
		OrgID:  orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows), nil
}

func toModelEvent(row sqlc.Event) *model.Event {
	return &model.Event{
		ID:        row.ID,
		TaskID:    row.TaskID,
		OrgID:     row.OrgID,
		Type:      row.Type,
		Wake:      row.Wake,
		Payload:   row.Payload,
		CreatedAt: row.CreatedAt,
	}
}

func toModelEvents(rows []sqlc.Event) []*model.Event {
	events := make([]*model.Event, len(rows))
	for i, row := range rows {
		events[i] = toModelEvent(row)
	}
	return events
}
