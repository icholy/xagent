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
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) ListTasks(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot list tasks"))
	}
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
	if !caller.Scopes.Allow(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot list tasks"))
	}
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

func (s *Server) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// No row exists yet, so authorize directly on the request attributes — the
	// narrow create scope (workspace/runner) a privileged caller holds. The
	// literal task.archived:"false" satisfies the minted scope's archived
	// predicate: a freshly created task is never archived. There is no row read to
	// fail fast before, so no AllowOp pre-gate is needed.
	if !caller.Scopes.Allow(authscope.OpTaskCreate,
		authscope.WithTaskWorkspace(req.Workspace),
		authscope.WithTaskRunner(req.Runner),
		authscope.WithTaskArchived(false),
	) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot create task"))
	}
	// Verify runner and workspace exist
	ok, err := s.store.HasWorkspace(ctx, nil, req.Runner, req.Workspace, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("workspace %q not found on runner %q", req.Workspace, req.Runner))
	}
	task := &model.Task{
		Name:      req.Name,
		Runner:    req.Runner,
		Workspace: req.Workspace,
		Status:    model.TaskStatusPending,
		Command:   model.TaskCommandStart,
		Version:   1,
		OrgID:     caller.OrgID,
	}
	if req.AutoArchive != nil {
		task.AutoArchive = req.AutoArchive.AsDuration()
	}
	err = s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.CreateTask(ctx, tx, task); err != nil {
			return err
		}
		// Seed the stream with the initial instructions as instruction events
		// instead of a tasks.instructions column. The task already starts via
		// Command=Start above; instruction events always wake (per the proposal's
		// type semantics).
		for _, inst := range req.Instructions {
			if err := s.store.CreateEvent(ctx, tx, &model.Event{
				TaskID: task.ID,
				OrgID:  task.OrgID,
				Wake:   true,
				Payload: &model.InstructionPayload{
					Text: inst.Text,
					URL:  inst.Url,
				},
			}); err != nil {
				return err
			}
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
		OrgID:          caller.OrgID,
		Runner:         task.PendingRunner(),
		UserID:         caller.ID,
		ClientID:       caller.ClientID,
		Time:           time.Now(),
		ChannelMessage: fmt.Sprintf("Task %d created on %s/%s.", task.ID, task.Runner, task.Workspace),
	})
	return &xagentv1.CreateTaskResponse{
		Task: task.Proto(s.baseURL),
	}, nil
}

func (s *Server) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate before the DB read. AllowOp ignores
	// predicates, so a narrow task.read:{task.id:N} holder still passes here; this
	// only rejects callers lacking the op entirely.
	if !caller.Scopes.AllowOp(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read task"))
	}
	task, err := s.store.GetTask(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	// The real instance check, after the row is loaded.
	if !caller.Scopes.Allow(authscope.OpTaskRead, task.ScopeAttr()...) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read task"))
	}
	return &xagentv1.GetTaskResponse{
		Task: task.Proto(s.baseURL),
	}, nil
}

func (s *Server) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.AllowOp(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read task"))
	}
	task, err := s.store.GetTask(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !caller.Scopes.Allow(authscope.OpTaskRead, task.ScopeAttr()...) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read task"))
	}
	// The brief: the task's to-agent events (instruction + external) in stream
	// order. Instructions are read from the stream, not a denormalized column.
	events, _ := s.store.ListTaskBrief(ctx, nil, req.Id, caller.OrgID)
	links, _ := s.store.ListLinksByTask(ctx, nil, req.Id, caller.OrgID)
	resp := &xagentv1.GetTaskDetailsResponse{
		Task:   task.Proto(s.baseURL),
		Events: model.ProtoMap(events),
		Links:  model.ProtoMap(links),
	}
	return resp, nil
}

func (s *Server) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate before entering the transaction (AllowOp
	// ignores predicates); the per-instance check runs inside the tx against the
	// row it already loads.
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	notification := model.Notification{
		Type:     "change",
		OrgID:    caller.OrgID,
		UserID:   caller.ID,
		ClientID: caller.ClientID,
		Time:     time.Now(),
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !caller.Scopes.Allow(authscope.OpTaskWrite, task.ScopeAttr()...) {
			return connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
		}
		var changed []string
		if req.Name != "" {
			task.Name = req.Name
			changed = append(changed, "name")
		}
		for _, inst := range req.AddInstructions {
			// Adding an instruction appends an instruction event (wake=true) to the
			// stream instead of mutating a tasks.instructions column. The actual
			// restart is driven by req.Start below via Task.Start(), exactly as before.
			if err := s.store.CreateEvent(ctx, tx, &model.Event{
				TaskID: task.ID,
				OrgID:  task.OrgID,
				Wake:   true,
				Payload: &model.InstructionPayload{
					Text: inst.Text,
					URL:  inst.Url,
				},
			}); err != nil {
				return err
			}
			changed = append(changed, "instructions")
		}
		if req.Start {
			task.Start()
			changed = append(changed, "status")
		}
		if req.AutoArchive != nil {
			task.AutoArchive = req.AutoArchive.AsDuration()
			changed = append(changed, "auto_archive")
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
		notification.Runner = task.PendingRunner()
		notification.Resources = []model.NotificationResource{
			{Action: "updated", Type: "task", ID: task.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		}
		if req.Start {
			notification.ChannelMessage = fmt.Sprintf("Task %d queued: %s.", task.ID, strings.Join(changed, ", "))
		}
		return tx.Commit()
	})
	if err != nil {
		// The in-tx instance check returns PermissionDenied; surface it as-is
		// rather than re-wrapping it as Internal.
		if connect.CodeOf(err) == connect.CodePermissionDenied {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task updated", "id", req.Id, "name", req.Name, "start", req.Start, "instructions_added", len(req.AddInstructions))
	s.publish(notification)
	return &xagentv1.UpdateTaskResponse{}, nil
}

func (s *Server) ArchiveTask(ctx context.Context, req *xagentv1.ArchiveTaskRequest) (*xagentv1.ArchiveTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate before entering the transaction (AllowOp
	// ignores predicates); the per-instance check runs inside the tx against the
	// row it already loads.
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	notification := model.Notification{
		Type:     "change",
		OrgID:    caller.OrgID,
		UserID:   caller.ID,
		ClientID: caller.ClientID,
		Time:     time.Now(),
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !caller.Scopes.Allow(authscope.OpTaskWrite, task.ScopeAttr()...) {
			return connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
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
		notification.Runner = task.PendingRunner()
		notification.Resources = []model.NotificationResource{
			{Action: "archived", Type: "task", ID: task.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		}
		notification.ChannelMessage = fmt.Sprintf("Task %d archived.", task.ID)
		return tx.Commit()
	})
	if err != nil {
		// The in-tx instance check returns PermissionDenied; surface it as-is
		// rather than re-wrapping it as Internal.
		if connect.CodeOf(err) == connect.CodePermissionDenied {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task archived", "id", req.Id)
	s.publish(notification)
	return &xagentv1.ArchiveTaskResponse{}, nil
}

func (s *Server) UnarchiveTask(ctx context.Context, req *xagentv1.UnarchiveTaskRequest) (*xagentv1.UnarchiveTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate before entering the transaction (AllowOp
	// ignores predicates); the per-instance check runs inside the tx against the
	// row it already loads.
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	notification := model.Notification{
		Type:     "change",
		OrgID:    caller.OrgID,
		UserID:   caller.ID,
		ClientID: caller.ClientID,
		Time:     time.Now(),
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !caller.Scopes.Allow(authscope.OpTaskWrite, task.ScopeAttr()...) {
			return connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
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
		notification.Runner = task.PendingRunner()
		notification.Resources = []model.NotificationResource{
			{Action: "unarchived", Type: "task", ID: task.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		}
		return tx.Commit()
	})
	if err != nil {
		// The in-tx instance check returns PermissionDenied; surface it as-is
		// rather than re-wrapping it as Internal.
		if connect.CodeOf(err) == connect.CodePermissionDenied {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task unarchived", "id", req.Id)
	s.publish(notification)
	return &xagentv1.UnarchiveTaskResponse{}, nil
}

func (s *Server) CancelTask(ctx context.Context, req *xagentv1.CancelTaskRequest) (*xagentv1.CancelTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate before entering the transaction (AllowOp
	// ignores predicates); the per-instance check runs inside the tx against the
	// row it already loads.
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	notification := model.Notification{
		Type:     "change",
		OrgID:    caller.OrgID,
		UserID:   caller.ID,
		ClientID: caller.ClientID,
		Time:     time.Now(),
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !caller.Scopes.Allow(authscope.OpTaskWrite, task.ScopeAttr()...) {
			return connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
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
		notification.Runner = task.PendingRunner()
		notification.Resources = []model.NotificationResource{
			{Action: "cancelled", Type: "task", ID: task.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		}
		// Only the Pending->Cancelled branch is terminal here; the Running->
		// Cancelling branch will produce its terminal "cancelled" message via
		// SubmitRunnerEvents once the runner stops the container.
		if task.Status == model.TaskStatusCancelled {
			notification.ChannelMessage = fmt.Sprintf("Task %d cancelled.", task.ID)
		}
		return tx.Commit()
	})
	if err != nil {
		// The in-tx instance check returns PermissionDenied; surface it as-is
		// rather than re-wrapping it as Internal.
		if connect.CodeOf(err) == connect.CodePermissionDenied {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task cancelled", "id", req.Id)
	s.publish(notification)
	return &xagentv1.CancelTaskResponse{}, nil
}

func (s *Server) RestartTask(ctx context.Context, req *xagentv1.RestartTaskRequest) (*xagentv1.RestartTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Coarse, fail-fast capability gate before entering the transaction (AllowOp
	// ignores predicates); the per-instance check runs inside the tx against the
	// row it already loads.
	if !caller.Scopes.AllowOp(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	notification := model.Notification{
		Type:     "change",
		OrgID:    caller.OrgID,
		UserID:   caller.ID,
		ClientID: caller.ClientID,
		Time:     time.Now(),
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !caller.Scopes.Allow(authscope.OpTaskWrite, task.ScopeAttr()...) {
			return connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
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
		notification.Runner = task.PendingRunner()
		notification.Resources = []model.NotificationResource{
			{Action: "restarted", Type: "task", ID: task.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		}
		notification.ChannelMessage = fmt.Sprintf("Task %d restart requested.", task.ID)
		return tx.Commit()
	})
	if err != nil {
		// The in-tx instance check returns PermissionDenied; surface it as-is
		// rather than re-wrapping it as Internal.
		if connect.CodeOf(err) == connect.CodePermissionDenied {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task restarted", "id", req.Id)
	s.publish(notification)
	return &xagentv1.RestartTaskResponse{}, nil
}
