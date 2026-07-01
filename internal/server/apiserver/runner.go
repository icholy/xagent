package apiserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) SubmitRunnerEvents(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate (AllowOp ignores predicates); each event
	// is authorized per-instance inside its transaction against the row it loads.
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot submit runner events"))
	}
	// Authorize per-event: a partial-batch failure is acceptable (this RPC is
	// runner-facing, and coarse/admin callers pass every instance check anyway).
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
		err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			task, err := s.store.GetTaskForUpdate(ctx, tx, event.TaskID, caller.OrgID)
			if err != nil {
				return err
			}
			if !caller.Scopes.Allow(authscope.OpTaskWrite, task.ScopeAttr()...) {
				return connect.NewError(connect.CodePermissionDenied, errors.New("cannot submit runner events"))
			}
			from := task.Status
			applied := task.ApplyRunnerEvent(&event)
			s.log.Info("runner event recieved",
				"task_id", event.TaskID,
				"event", event.Event,
				"version", event.Version,
				"status", task.Status,
				"applied", applied,
			)
			if !applied {
				notification.Ignore = true
				return nil
			}
			if err := s.store.UpdateTask(ctx, tx, task); err != nil {
				return err
			}
			// Append the sandbox lifecycle event beside the status fold (status is
			// the materialized projection; the fold logic is unchanged). from is the
			// status before ApplyRunnerEvent; the task carries the post-fold status.
			// Attach it as the notification's causal event when one is produced;
			// consumers derive terminal-outcome semantics from its ToStatus (see
			// LifecyclePayload.IsTerminal) instead of a hand-rolled status field.
			if ev, ok := event.LifecycleEvent(task, from); ok {
				if err := s.store.CreateEvent(ctx, tx, ev); err != nil {
					return err
				}
				notification.TaskEvent = ev
			}
			notification.Runner = task.PendingRunner()
			return tx.Commit()
		})
		if err != nil {
			// The in-tx checks return typed connect errors; surface any of them
			// as-is rather than re-wrapping them as Internal.
			if connect.CodeOf(err) != connect.CodeUnknown {
				return nil, err
			}
			if errors.Is(err, sql.ErrNoRows) {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", event.TaskID))
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.publish(notification)
	}
	return &xagentv1.SubmitRunnerEventsResponse{}, nil
}
