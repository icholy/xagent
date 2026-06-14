package apiserver

import (
	"cmp"
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

func (s *Server) ListEvents(ctx context.Context, req *xagentv1.ListEventsRequest) (*xagentv1.ListEventsResponse, error) {
	const maxLimit = 100
	limit := cmp.Or(int(req.Limit), maxLimit)
	if limit < 0 || limit > maxLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("limit must be at most %d", maxLimit))
	}
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpEventRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot list events"))
	}
	events, err := s.store.ListEvents(ctx, nil, limit, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListEventsResponse{
		Events: model.ProtoMap(events),
	}, nil
}

func (s *Server) CreateEvent(ctx context.Context, req *xagentv1.CreateEventRequest) (*xagentv1.CreateEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Events are task-scoped: creating one writes the target task too, so both
	// halves must pass — create the event (coarse) and write the task it belongs
	// to (per-instance).
	if !caller.Scopes.Allow(authscope.OpEventCreate) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot create event"))
	}
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	// Verify task ownership and the per-instance task scope.
	task, err := s.store.GetTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !caller.Scopes.Allow(authscope.OpTaskWrite, task.ScopeAttr()...) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	// A user-created event maps to the external arm; it does not wake the task.
	event, err := model.NewExternalEvent(task.ID, caller.OrgID, false, &xagentv1.ExternalPayload{
		Description: req.Description,
		Url:         req.Url,
		Data:        req.Data,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.store.CreateEvent(ctx, nil, event); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event created", "id", event.ID, "task_id", event.TaskID, "type", event.Type)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "event", ID: event.ID},
			{Action: "updated", Type: "task", ID: event.TaskID},
		},
		OrgID:    caller.OrgID,
		UserID:   caller.ID,
		ClientID: caller.ClientID,
		Time:     time.Now(),
	})
	return &xagentv1.CreateEventResponse{
		Event: event.Proto(),
	}, nil
}

func (s *Server) GetEvent(ctx context.Context, req *xagentv1.GetEventRequest) (*xagentv1.GetEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpEventRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read event"))
	}
	event, err := s.store.GetEvent(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GetEventResponse{
		Event: event.Proto(),
	}, nil
}

func (s *Server) DeleteEvent(ctx context.Context, req *xagentv1.DeleteEventRequest) (*xagentv1.DeleteEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpEventWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write event"))
	}
	if err := s.store.DeleteEvent(ctx, nil, req.Id, caller.OrgID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event deleted", "id", req.Id)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "deleted", Type: "event", ID: req.Id}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.DeleteEventResponse{}, nil
}

func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.AllowOp(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read task"))
	}
	// A blanket task.read (admin/coarse) is authorized without inspecting the row,
	// and the list query is already org-scoped. Only a predicated caller needs the
	// row loaded to check task.id/parent/archived.
	if !caller.Scopes.Allow(authscope.OpTaskRead) {
		task, err := s.store.GetTask(ctx, nil, req.TaskId, caller.OrgID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if !caller.Scopes.Allow(authscope.OpTaskRead, task.ScopeAttr()...) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read task"))
		}
	}
	events, err := s.store.ListEventsByTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListEventsByTaskResponse{
		Events: model.ProtoMap(events),
	}, nil
}
