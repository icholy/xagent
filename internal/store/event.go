package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) queries(tx *sql.Tx) *sqlc.Queries {
	if tx != nil {
		return sqlc.New(tx)
	}
	return sqlc.New(r.db)
}

func (r *EventRepository) Create(ctx context.Context, tx *sql.Tx, event *model.Event) error {
	id, err := r.queries(tx).CreateEvent(ctx, sqlc.CreateEventParams{
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

func (r *EventRepository) Get(ctx context.Context, tx *sql.Tx, id int64, owner string) (*model.Event, error) {
	row, err := r.queries(tx).GetEvent(ctx, sqlc.GetEventParams{
		ID:    id,
		Owner: owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelEvent(row), nil
}

func (r *EventRepository) HasEvent(ctx context.Context, tx *sql.Tx, id int64, owner string) (bool, error) {
	exists, err := r.queries(tx).HasEvent(ctx, sqlc.HasEventParams{
		ID:    id,
		Owner: owner,
	})
	return exists != 0, err
}

func (r *EventRepository) List(ctx context.Context, tx *sql.Tx, limit int, owner string) ([]*model.Event, error) {
	rows, err := r.queries(tx).ListEvents(ctx, sqlc.ListEventsParams{
		Owner: owner,
		Limit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows), nil
}

func (r *EventRepository) FindByURL(ctx context.Context, tx *sql.Tx, url string) ([]*model.Event, error) {
	rows, err := r.queries(tx).FindEventsByURL(ctx, sql.NullString{String: url, Valid: url != ""})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows), nil
}

func (r *EventRepository) Delete(ctx context.Context, tx *sql.Tx, id int64, owner string) error {
	return WithTx(ctx, r.db, tx, func(tx *sql.Tx) error {
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

func (r *EventRepository) AddTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	return r.queries(tx).AddEventTask(ctx, sqlc.AddEventTaskParams{
		EventID: eventID,
		TaskID:  taskID,
	})
}

func (r *EventRepository) RemoveTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
	return r.queries(tx).RemoveEventTask(ctx, sqlc.RemoveEventTaskParams{
		EventID: eventID,
		TaskID:  taskID,
	})
}

func (r *EventRepository) ListTasks(ctx context.Context, tx *sql.Tx, eventID int64, owner string) ([]int64, error) {
	return r.queries(tx).ListEventTasks(ctx, sqlc.ListEventTasksParams{
		EventID: eventID,
		Owner:   owner,
	})
}

func (r *EventRepository) ListByTask(ctx context.Context, tx *sql.Tx, taskID int64, owner string) ([]*model.Event, error) {
	rows, err := r.queries(tx).ListEventsByTask(ctx, sqlc.ListEventsByTaskParams{
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
