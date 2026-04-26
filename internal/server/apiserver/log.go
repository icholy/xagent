package apiserver

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	for _, entry := range req.Entries {
		log := model.LogFromProto(entry)
		log.TaskID = req.TaskId
		if err := s.store.CreateLog(ctx, nil, &log); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "appended", Type: "task_logs", ID: req.TaskId}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID: caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.UploadLogsResponse{}, nil
}

func (s *Server) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	logs, err := s.store.ListLogsByTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListLogsResponse{
		Entries: model.ProtoMap(logs),
	}, nil
}
