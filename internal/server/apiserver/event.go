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
	events, err := s.store.ListEvents(ctx, nil, limit, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListEventsResponse{
		Events: make([]*xagentv1.Event, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = e.Proto()
	}
	return resp, nil
}

func (s *Server) CreateEvent(ctx context.Context, req *xagentv1.CreateEventRequest) (*xagentv1.CreateEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
	event := &model.Event{
		Description: req.Description,
		Data:        req.Data,
		URL:         req.Url,
		OrgID:       caller.OrgID,
	}
	if err := s.store.CreateEvent(ctx, nil, event); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event created", "id", event.ID, "description", event.Description)
	s.publish(caller.ID, model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "created", Type: "event", ID: event.ID}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.CreateEventResponse{
		Event: event.Proto(),
	}, nil
}

func (s *Server) GetEvent(ctx context.Context, req *xagentv1.GetEventRequest) (*xagentv1.GetEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
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
	if err := s.store.DeleteEvent(ctx, nil, req.Id, caller.OrgID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event deleted", "id", req.Id)
	s.publish(caller.ID, model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "deleted", Type: "event", ID: req.Id}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.DeleteEventResponse{}, nil
}

func (s *Server) AddEventTask(ctx context.Context, req *xagentv1.AddEventTaskRequest) (*xagentv1.AddEventTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	// Verify event ownership
	ok, err = s.store.HasEvent(ctx, nil, req.EventId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.EventId))
	}
	if err := s.store.AddEventTask(ctx, nil, req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task added", "event_id", req.EventId, "task_id", req.TaskId)
	s.publish(caller.ID, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: req.TaskId},
			{Action: "updated", Type: "event", ID: req.EventId},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.AddEventTaskResponse{}, nil
}

func (s *Server) RemoveEventTask(ctx context.Context, req *xagentv1.RemoveEventTaskRequest) (*xagentv1.RemoveEventTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	// Verify event ownership
	ok, err = s.store.HasEvent(ctx, nil, req.EventId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.EventId))
	}
	if err := s.store.RemoveEventTask(ctx, nil, req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task removed", "event_id", req.EventId, "task_id", req.TaskId)
	s.publish(caller.ID, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: req.TaskId},
			{Action: "updated", Type: "event", ID: req.EventId},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.RemoveEventTaskResponse{}, nil
}

func (s *Server) ListEventTasks(ctx context.Context, req *xagentv1.ListEventTasksRequest) (*xagentv1.ListEventTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	taskIDs, err := s.store.ListEventTasks(ctx, nil, req.EventId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListEventTasksResponse{TaskIds: taskIDs}, nil
}

func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	events, err := s.store.ListEventsByTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListEventsByTaskResponse{
		Events: make([]*xagentv1.Event, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = e.Proto()
	}
	return resp, nil
}
