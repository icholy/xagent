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

// reportLogType is the legacy logs.type value the agent's report tool uploads.
// The UploadLogs handler re-points it onto the event stream as a report event;
// the wire (UploadLogs) is unchanged until the agent surface lands.
const reportLogType = "llm"

func (s *Server) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate before the DB read (AllowOp ignores
	// predicates); the instance check happens after the row is loaded.
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
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
	for _, entry := range req.Entries {
		// The report channel moved to the event stream: an `llm` entry (the
		// agent's report tool) becomes a from-agent `report` event rather than a
		// logs row. The other log types stay in the logs table until the
		// drop-logs increment. Reports are from-agent, so they do not wake the task.
		if entry.Type == reportLogType {
			if err := s.store.CreateEvent(ctx, nil, &model.Event{
				TaskID:  task.ID,
				OrgID:   task.OrgID,
				Payload: &model.ReportPayload{Content: entry.Content},
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			continue
		}
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
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.UploadLogsResponse{}, nil
}

func (s *Server) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
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
	logs, err := s.store.ListLogsByTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListLogsResponse{
		Entries: model.ProtoMap(logs),
	}, nil
}
