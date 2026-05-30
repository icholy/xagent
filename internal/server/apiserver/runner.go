package apiserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) SubmitRunnerEvents(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	for _, pbEvent := range req.Events {
		event := model.RunnerEventFromProto(pbEvent)
		kind, ok := runnerEventKind(event.Event)
		if !ok {
			continue
		}
		change := model.TaskChange{
			TaskID: event.TaskID,
			Kind:   kind,
			Actor: model.Actor{
				Kind: model.ActorKindRunner,
				Name: caller.DisplayName(),
				ID:   caller.ID,
			},
			Exit: &model.ExitInfo{Event: event.Event},
			Time: time.Now(),
		}
		var applied bool
		var runner string
		err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			task, err := s.store.GetTaskForUpdate(ctx, tx, event.TaskID, caller.OrgID)
			if err != nil {
				return err
			}
			applied = task.ApplyRunnerEvent(&event)
			s.log.Info("runner event recieved",
				"task_id", event.TaskID,
				"event", event.Event,
				"version", event.Version,
				"status", task.Status,
				"applied", applied,
			)
			if !applied {
				return nil
			}
			if err := s.store.UpdateTask(ctx, tx, task); err != nil {
				return err
			}
			change.Status = task.Status
			runner = task.PendingRunner()
			logRow := change.Log()
			if err := s.store.CreateLog(ctx, tx, &logRow); err != nil {
				return err
			}
			return tx.Commit()
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", event.TaskID))
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if applied {
			s.publish(change.Notification(model.Envelope{
				OrgID:    caller.OrgID,
				UserID:   caller.ID,
				ClientID: caller.ClientID,
				Runner:   runner,
			}))
		}
	}
	return &xagentv1.SubmitRunnerEventsResponse{}, nil
}

func runnerEventKind(e model.RunnerEventType) (model.TaskChangeKind, bool) {
	switch e {
	case model.RunnerEventStarted:
		return model.TaskChangeContainerStarted, true
	case model.RunnerEventStopped:
		return model.TaskChangeContainerExited, true
	case model.RunnerEventFailed:
		return model.TaskChangeContainerFailed, true
	default:
		return 0, false
	}
}
