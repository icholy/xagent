package apiserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) ListTasks(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	tasks, err := s.store.ListTasks(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto(s.baseURL)
	}
	return resp, nil
}

func (s *Server) ListRunnerTasks(ctx context.Context, req *xagentv1.ListRunnerTasksRequest) (*xagentv1.ListRunnerTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	tasks, err := s.store.ListTasksForRunner(ctx, nil, req.Runner, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &xagentv1.ListRunnerTasksResponse{}, nil
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListRunnerTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto(s.baseURL)
	}
	return resp, nil
}

func (s *Server) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	tasks, err := s.store.ListTaskChildren(ctx, nil, req.ParentId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListChildTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto(s.baseURL)
	}
	return resp, nil
}

func (s *Server) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify parent task ownership if specified
	if req.Parent != 0 {
		ok, err := s.store.HasTask(ctx, nil, req.Parent, caller.OrgID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("parent task %d not found", req.Parent))
		}
	}
	// Verify runner and workspace exist
	ok, err := s.store.HasWorkspace(ctx, nil, req.Runner, req.Workspace, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("workspace %q not found on runner %q", req.Workspace, req.Runner))
	}
	instructions := make([]model.Instruction, len(req.Instructions))
	for i, inst := range req.Instructions {
		instructions[i] = model.InstructionFromProto(inst)
	}
	task := &model.Task{
		Name:         req.Name,
		Parent:       req.Parent,
		Runner:       req.Runner,
		Workspace:    req.Workspace,
		Instructions: instructions,
		Status:       model.TaskStatusPending,
		Command:      model.TaskCommandStart,
		Version:      1,
		OrgID:        caller.OrgID,
	}
	err = s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.CreateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  task.ID,
			Type:    "audit",
			Content: fmt.Sprintf("%s created task", caller.AuditName()),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task created", "id", task.ID, "runner", task.Runner, "workspace", task.Workspace, "org_id", task.OrgID)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "task", ID: task.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		},
		OrgID:  caller.OrgID,
		UserID: caller.ID,
		Time:   time.Now(),
	})
	return &xagentv1.CreateTaskResponse{
		Task: task.Proto(s.baseURL),
	}, nil
}

func (s *Server) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	task, err := s.store.GetTask(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GetTaskResponse{
		Task: task.Proto(s.baseURL),
	}, nil
}

func (s *Server) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	task, err := s.store.GetTask(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	children, _ := s.store.ListTaskChildren(ctx, nil, req.Id, caller.OrgID)
	events, _ := s.store.ListEventsByTask(ctx, nil, req.Id, caller.OrgID)
	links, _ := s.store.ListLinksByTask(ctx, nil, req.Id, caller.OrgID)
	resp := &xagentv1.GetTaskDetailsResponse{
		Task:     task.Proto(s.baseURL),
		Children: make([]*xagentv1.Task, len(children)),
		Events:   model.MapProtos(events),
		Links:    model.MapProtos(links),
	}
	for i, c := range children {
		resp.Children[i] = c.Proto(s.baseURL)
	}
	return resp, nil
}

func (s *Server) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		var changed []string
		if req.Name != "" {
			task.Name = req.Name
			changed = append(changed, "name")
		}
		for _, inst := range req.AddInstructions {
			task.Instructions = append(task.Instructions, model.InstructionFromProto(inst))
			changed = append(changed, "instructions")
		}
		if req.Start {
			task.Start()
			changed = append(changed, "status")
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s updated task: %s", caller.AuditName(), strings.Join(changed, ", ")),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task updated", "id", req.Id, "name", req.Name, "start", req.Start, "instructions_added", len(req.AddInstructions))
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID:  caller.OrgID,
		UserID: caller.ID,
		Time:   time.Now(),
	})
	return &xagentv1.UpdateTaskResponse{}, nil
}

func (s *Server) ArchiveTask(ctx context.Context, req *xagentv1.ArchiveTaskRequest) (*xagentv1.ArchiveTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Archive() {
			return fmt.Errorf("cannot archive task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s archived task", caller.AuditName()),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task archived", "id", req.Id)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "archived", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID:  caller.OrgID,
		UserID: caller.ID,
		Time:   time.Now(),
	})
	return &xagentv1.ArchiveTaskResponse{}, nil
}

func (s *Server) UnarchiveTask(ctx context.Context, req *xagentv1.UnarchiveTaskRequest) (*xagentv1.UnarchiveTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Unarchive() {
			return fmt.Errorf("cannot unarchive task: not archived")
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s unarchived task", caller.AuditName()),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task unarchived", "id", req.Id)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "unarchived", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID:  caller.OrgID,
		UserID: caller.ID,
		Time:   time.Now(),
	})
	return &xagentv1.UnarchiveTaskResponse{}, nil
}

func (s *Server) CancelTask(ctx context.Context, req *xagentv1.CancelTaskRequest) (*xagentv1.CancelTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Cancel() {
			return fmt.Errorf("cannot cancel task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s cancelled task", caller.AuditName()),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task cancelled", "id", req.Id)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "cancelled", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID:  caller.OrgID,
		UserID: caller.ID,
		Time:   time.Now(),
	})
	return &xagentv1.CancelTaskResponse{}, nil
}

func (s *Server) RestartTask(ctx context.Context, req *xagentv1.RestartTaskRequest) (*xagentv1.RestartTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Restart() {
			return fmt.Errorf("cannot restart task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s restarted task", caller.AuditName()),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task restarted", "id", req.Id)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "restarted", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID:  caller.OrgID,
		UserID: caller.ID,
		Time:   time.Now(),
	})
	return &xagentv1.RestartTaskResponse{}, nil
}
