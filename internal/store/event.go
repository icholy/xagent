package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateEvent(ctx context.Context, tx *sql.Tx, event *model.Event) error {
	id, err := s.queries(tx).CreateEvent(ctx, sqlc.CreateEventParams{
		Description: event.Description,
		Data:        event.Data,
		Url:         sql.NullString{String: event.URL, Valid: event.URL != ""},
		Owner:       event.Owner,
		CreatedAt:   sql.NullTime{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return err
	}
	event.ID = id
	return nil
}

func (s *Store) GetEvent(ctx context.Context, tx *sql.Tx, id int64, owner string) (*model.Event, error) {
	row, err := s.queries(tx).GetEvent(ctx, sqlc.GetEventParams{
		ID:    id,
		Owner: owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelEvent(row), nil
}

func (s *Store) HasEvent(ctx context.Context, tx *sql.Tx, id int64, owner string) (bool, error) {
	exists, err := s.queries(tx).HasEvent(ctx, sqlc.HasEventParams{
		ID:    id,
		Owner: owner,
	})
	return exists != 0, err
}

func (s *Store) ListEvents(ctx context.Context, tx *sql.Tx, limit int, owner string) ([]*model.Event, error) {
	rows, err := s.queries(tx).ListEvents(ctx, sqlc.ListEventsParams{
		Owner: owner,
		Limit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows), nil
}

func (s *Store) FindEventsByURL(ctx context.Context, tx *sql.Tx, url string) ([]*model.Event, error) {
	rows, err := s.queries(tx).FindEventsByURL(ctx, sql.NullString{String: url, Valid: url != ""})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows), nil
}

func (s *Store) DeleteEvent(ctx context.Context, tx *sql.Tx, id int64, owner string) error {
	return s.WithTx(ctx, tx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if err := q.DeleteEventTasks(ctx, id); err != nil {
			return err
		}
		if err := q.DeleteEvent(ctx, sqlc.DeleteEventParams{ID: id, Owner: owner}); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *Store) AddEventTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	return s.queries(tx).AddEventTask(ctx, sqlc.AddEventTaskParams{
		EventID: eventID,
		TaskID:  taskID,
	})
}

func (s *Store) RemoveEventTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	return s.queries(tx).RemoveEventTask(ctx, sqlc.RemoveEventTaskParams{
		EventID: eventID,
		TaskID:  taskID,
	})
}

func (s *Store) ListEventTasks(ctx context.Context, tx *sql.Tx, eventID int64, owner string) ([]int64, error) {
	return s.queries(tx).ListEventTasks(ctx, sqlc.ListEventTasksParams{
		EventID: eventID,
		Owner:   owner,
	})
}

func (s *Store) ListEventsByTask(ctx context.Context, tx *sql.Tx, taskID int64, owner string) ([]*model.Event, error) {
	rows, err := s.queries(tx).ListEventsByTask(ctx, sqlc.ListEventsByTaskParams{
		TaskID: taskID,
		Owner:  owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows), nil
}

func toModelEvent(row sqlc.Event) *model.Event {
	return &model.Event{
		ID:          row.ID,
		Description: row.Description,
		Data:        row.Data,
		URL:         row.Url.String,
		Owner:       row.Owner,
		CreatedAt:   row.CreatedAt.Time,
	}
}

func toModelEvents(rows []sqlc.Event) []*model.Event {
	events := make([]*model.Event, len(rows))
	for i, row := range rows {
		events[i] = toModelEvent(row)
	}
	return events
}
