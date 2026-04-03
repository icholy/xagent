package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateTaskEvent(ctx context.Context, tx *sql.Tx, event *model.TaskEvent) error {
	meta, err := json.Marshal(event.Meta)
	if err != nil {
		return err
	}
	id, err := s.q(tx).CreateTaskEvent(ctx, sqlc.CreateTaskEventParams{
		TaskID:    event.TaskID,
		Type:      event.Type,
		Content:   event.Content,
		Meta:      meta,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	event.ID = id
	return nil
}

func (s *Store) PollTaskEvents(ctx context.Context, tx *sql.Tx, taskID int64, afterID int64, limit int) ([]*model.TaskEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.q(tx).PollTaskEvents(ctx, sqlc.PollTaskEventsParams{
		TaskID: taskID,
		ID:     afterID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, err
	}
	events := make([]*model.TaskEvent, len(rows))
	for i, row := range rows {
		events[i] = toModelTaskEvent(row)
	}
	return events, nil
}

func (s *Store) DeleteTaskEventsByTask(ctx context.Context, tx *sql.Tx, taskID int64) error {
	return s.q(tx).DeleteTaskEventsByTask(ctx, taskID)
}

func toModelTaskEvent(row sqlc.TaskEvent) *model.TaskEvent {
	var meta map[string]string
	_ = json.Unmarshal(row.Meta, &meta)
	return &model.TaskEvent{
		ID:        row.ID,
		TaskID:    row.TaskID,
		Type:      row.Type,
		Content:   row.Content,
		Meta:      meta,
		CreatedAt: row.CreatedAt,
	}
}
