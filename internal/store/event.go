package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateEvent(ctx context.Context, tx *sql.Tx, event *model.Event) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	id, err := s.q(tx).CreateEvent(ctx, sqlc.CreateEventParams{
		TaskID:    event.TaskID,
		OrgID:     event.OrgID,
		Type:      event.Payload.Type(),
		Wake:      event.Wake,
		Payload:   payload,
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
	return toModelEvent(row)
}

func (s *Store) ListEvents(ctx context.Context, tx *sql.Tx, limit int, orgID int64) ([]*model.Event, error) {
	rows, err := s.q(tx).ListEvents(ctx, sqlc.ListEventsParams{
		OrgID: orgID,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows)
}

func (s *Store) DeleteEvent(ctx context.Context, tx *sql.Tx, id int64, orgID int64) error {
	return s.q(tx).DeleteEvent(ctx, sqlc.DeleteEventParams{ID: id, OrgID: orgID})
}

// ListEventsByTask returns a task's events in chronological stream order. A nil
// or empty types filter returns all events; a non-empty filter narrows to those
// event types — e.g. the agent's brief passes instruction + external.
func (s *Store) ListEventsByTask(ctx context.Context, tx *sql.Tx, taskID int64, orgID int64, types []string) ([]*model.Event, error) {
	// A nil slice encodes as SQL NULL (cardinality(NULL) is NULL, not 0), which
	// would filter everything out; coerce to an empty array so the "all types"
	// case matches the query's cardinality(...) = 0 guard.
	if types == nil {
		types = []string{}
	}
	rows, err := s.q(tx).ListEventsByTask(ctx, sqlc.ListEventsByTaskParams{
		TaskID: taskID,
		OrgID:  orgID,
		Types:  types,
	})
	if err != nil {
		return nil, err
	}
	return toModelEvents(rows)
}

func toModelEvent(row sqlc.Event) (*model.Event, error) {
	payload, err := toEventPayload(row.Type, row.Payload)
	if err != nil {
		return nil, err
	}
	return &model.Event{
		ID:        row.ID,
		TaskID:    row.TaskID,
		OrgID:     row.OrgID,
		Wake:      row.Wake,
		Payload:   payload,
		CreatedAt: row.CreatedAt,
	}, nil
}

func toModelEvents(rows []sqlc.Event) ([]*model.Event, error) {
	events := make([]*model.Event, len(rows))
	for i, row := range rows {
		event, err := toModelEvent(row)
		if err != nil {
			return nil, err
		}
		events[i] = event
	}
	return events, nil
}

// toEventPayload picks the concrete model.EventPayload for a stored row by
// switching on its type discriminator, then decodes the jsonb body into it.
// This is the only place the events.type column is consumed — it is a storage
// detail, not a field on the Event value.
func toEventPayload(typ string, data []byte) (model.EventPayload, error) {
	switch typ {
	case model.EventTypeInstruction:
		var p model.InstructionPayload
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("unmarshal instruction payload: %w", err)
		}
		return &p, nil
	case model.EventTypeExternal:
		var p model.ExternalPayload
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("unmarshal external payload: %w", err)
		}
		return &p, nil
	case model.EventTypeLink:
		var p model.LinkPayload
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("unmarshal link payload: %w", err)
		}
		return &p, nil
	default:
		return nil, fmt.Errorf("unknown event type %q", typ)
	}
}
