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
		notification := model.Notification{
			Type: "change",
			Resources: []model.NotificationResource{
				{Action: "updated", Type: "task", ID: event.TaskID},
				{Action: "appended", Type: "task_logs", ID: event.TaskID},
			},
			OrgID:    caller.OrgID,
			UserID:   caller.ID,
			ClientID: caller.ClientID,
			Time:     time.Now(),
		}
		var applied bool
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
			if log, ok := s.toRunnerEventLog(event); ok {
				if err := s.store.CreateLog(ctx, tx, &log); err != nil {
					return err
				}
			}
			notification.Runner = task.PendingRunner()
			// Only terminal statuses produce a channel message. Completed,
			// Failed, and Cancelled are unambiguous and agent-actionable; the
			// non-terminal runner transitions (running, restarting, pending
			// re-queue) don't carry enough context to say anything useful
			// without re-deriving why, so we stay silent and let the
			// eventual terminal event speak.
			switch task.Status {
			case model.TaskStatusCompleted:
				notification.ChannelMessage = fmt.Sprintf("Task %d completed.", task.ID)
			case model.TaskStatusFailed:
				notification.ChannelMessage = fmt.Sprintf("Task %d failed.", task.ID)
			case model.TaskStatusCancelled:
				notification.ChannelMessage = fmt.Sprintf("Task %d cancelled.", task.ID)
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
			s.publish(notification)
		}
	}
	return &xagentv1.SubmitRunnerEventsResponse{}, nil
}

func (s *Server) toRunnerEventLog(e model.RunnerEvent) (model.Log, bool) {
	switch e.Event {
	case model.RunnerEventStarted:
		return model.Log{
			TaskID:  e.TaskID,
			Type:    "info",
			Content: "container started",
		}, true
	case model.RunnerEventStopped:
		return model.Log{
			TaskID:  e.TaskID,
			Type:    "info",
			Content: "container exited successfully",
		}, true
	case model.RunnerEventFailed:
		return model.Log{
			TaskID:  e.TaskID,
			Type:    "error",
			Content: "container failed",
		}, true
	default:
		return model.Log{}, false
	}
}
