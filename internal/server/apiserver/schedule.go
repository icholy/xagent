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

// Schedules are org-owned objects; permissions gate the API surface, not the
// firing (see proposals/accepted/scheduled-tasks.md §"API surface"). Creating or
// updating a schedule is a deferred CreateTask, so it demands the same
// task-create scope on the target (workspace, runner). Deleting or toggling one
// is a mutation, gated on task-write like every other mutation in the codebase
// (there is no task-delete tier — write is it). Listing and getting only require
// org membership, expressed as the task-read capability every member holds.

func (s *Server) CreateSchedule(ctx context.Context, req *xagentv1.CreateScheduleRequest) (*xagentv1.CreateScheduleResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// No row exists yet, so authorize directly on the request's target — the
	// narrow create scope (workspace/runner) a privileged caller holds, exactly
	// as CreateTask does. A schedule is never archived, so the literal
	// task.archived:"false" satisfies the minted scope's archived predicate.
	if !caller.Scopes.Allow(authscope.OpTaskCreate,
		authscope.WithTaskWorkspace(req.Workspace),
		authscope.WithTaskRunner(req.Runner),
		authscope.WithTaskArchived(false),
	) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot create schedule"))
	}
	ok, err := s.store.HasWorkspace(ctx, nil, req.Runner, req.Workspace, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("workspace %q not found on runner %q", req.Workspace, req.Runner))
	}
	sched := &model.Schedule{
		OrgID:        caller.OrgID,
		CreatedBy:    caller.ID,
		Name:         req.Name,
		Workspace:    req.Workspace,
		Runner:       req.Runner,
		Namespace:    req.Namespace,
		Instructions: model.ScheduleInstructionsFromProto(req.Instructions),
		CronExpr:     req.CronExpr,
		Timezone:     cmp.Or(req.Timezone, "UTC"),
		Enabled:      req.Enabled,
	}
	if req.AutoArchive != nil {
		sched.AutoArchive = req.AutoArchive.AsDuration()
	}
	// Validate cron/timezone before the write so an invalid schedule can never be
	// stored; a parse error surfaces as InvalidArgument.
	if err := sched.Validate(); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	// An enabled schedule needs its first fire time; a disabled one stays out of
	// the claim query with next_run_at = NULL.
	if sched.Enabled {
		next, err := sched.Next(time.Now())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		sched.NextRunAt = &next
	}
	if err := s.store.CreateSchedule(ctx, nil, sched); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.InfoContext(ctx, "schedule created", "id", sched.ID, "runner", sched.Runner, "workspace", sched.Workspace)
	// A schedule change only refreshes the Web UI; it never wakes a runner (the
	// scheduler worker, L4, creates the tasks), so no Runner is set.
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "created", Type: "schedule", ID: sched.ID}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.CreateScheduleResponse{Schedule: sched.Proto()}, nil
}

func (s *Server) GetSchedule(ctx context.Context, req *xagentv1.GetScheduleRequest) (*xagentv1.GetScheduleResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read schedule"))
	}
	sched, err := s.store.GetSchedule(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("schedule %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GetScheduleResponse{Schedule: sched.Proto()}, nil
}

func (s *Server) ListSchedules(ctx context.Context, req *xagentv1.ListSchedulesRequest) (*xagentv1.ListSchedulesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot list schedules"))
	}
	scheds, err := s.store.ListSchedules(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListSchedulesResponse{Schedules: model.ProtoMap(scheds)}, nil
}

// UpdateSchedule replaces the mutable template + spec fields wholesale (the
// edit form sends the full desired state); enable/disable stays in
// SetScheduleEnabled. Authorizing already forces the caller to supply the target
// (workspace, runner), so the request is inherently a full specification.
func (s *Server) UpdateSchedule(ctx context.Context, req *xagentv1.UpdateScheduleRequest) (*xagentv1.UpdateScheduleResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Updating retargets a deferred CreateTask, so authorize the create scope on
	// the new (workspace, runner) up front. This also denies an empty-scopes
	// caller before any store access, keeping the row read out of that path.
	if !caller.Scopes.Allow(authscope.OpTaskCreate,
		authscope.WithTaskWorkspace(req.Workspace),
		authscope.WithTaskRunner(req.Runner),
		authscope.WithTaskArchived(false),
	) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot update schedule"))
	}
	ok, err := s.store.HasWorkspace(ctx, nil, req.Runner, req.Workspace, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("workspace %q not found on runner %q", req.Workspace, req.Runner))
	}
	timezone := cmp.Or(req.Timezone, "UTC")
	var sched *model.Schedule
	err = s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		existing, err := s.store.GetScheduleForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		existing.Name = req.Name
		existing.Workspace = req.Workspace
		existing.Runner = req.Runner
		existing.Namespace = req.Namespace
		existing.Instructions = model.ScheduleInstructionsFromProto(req.Instructions)
		existing.CronExpr = req.CronExpr
		existing.Timezone = timezone
		existing.AutoArchive = req.AutoArchive.AsDuration()
		if err := existing.Validate(); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		// An enabled schedule re-aligns to its cron grid from now on every update,
		// so a spec edit takes effect immediately; a disabled one keeps
		// next_run_at = NULL (enable/disable is SetScheduleEnabled's job).
		if existing.Enabled {
			next, err := existing.Next(time.Now())
			if err != nil {
				return connect.NewError(connect.CodeInvalidArgument, err)
			}
			existing.NextRunAt = &next
		}
		if err := s.store.UpdateSchedule(ctx, tx, existing); err != nil {
			return err
		}
		sched = existing
		return tx.Commit()
	})
	if err != nil {
		// The in-tx checks return typed connect errors; surface any of them as-is
		// rather than re-wrapping them as Internal.
		if connect.CodeOf(err) != connect.CodeUnknown {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("schedule %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.InfoContext(ctx, "schedule updated", "id", sched.ID, "runner", sched.Runner, "workspace", sched.Workspace)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "updated", Type: "schedule", ID: sched.ID}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.UpdateScheduleResponse{Schedule: sched.Proto()}, nil
}

func (s *Server) DeleteSchedule(ctx context.Context, req *xagentv1.DeleteScheduleRequest) (*xagentv1.DeleteScheduleResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Deleting is a mutation, gated on task-write like ArchiveTask/DeleteEvent —
	// a read-only caller must not be able to remove an org schedule.
	if !caller.Scopes.Allow(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot delete schedule"))
	}
	// DeleteSchedule is org-scoped, so a missing (or other-org) id is a no-op,
	// matching DeleteEvent.
	if err := s.store.DeleteSchedule(ctx, nil, req.Id, caller.OrgID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.InfoContext(ctx, "schedule deleted", "id", req.Id)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "deleted", Type: "schedule", ID: req.Id}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.DeleteScheduleResponse{}, nil
}

func (s *Server) SetScheduleEnabled(ctx context.Context, req *xagentv1.SetScheduleEnabledRequest) (*xagentv1.SetScheduleEnabledResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Toggling a schedule is a mutation — and enabling resumes firing on a target
	// the caller may not hold create scope for — so it is gated on task-write, the
	// mutation tier, not on read/membership.
	if !caller.Scopes.Allow(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot update schedule"))
	}
	var sched *model.Schedule
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		existing, err := s.store.GetScheduleForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		existing.Enabled = req.Enabled
		if req.Enabled {
			// Enabling realigns to the grid from now; a stored cron/timezone that has
			// since become invalid surfaces as InvalidArgument.
			next, err := existing.Next(time.Now())
			if err != nil {
				return connect.NewError(connect.CodeInvalidArgument, err)
			}
			existing.NextRunAt = &next
		} else {
			// Disabling drops it from the claim query.
			existing.NextRunAt = nil
		}
		if err := s.store.UpdateSchedule(ctx, tx, existing); err != nil {
			return err
		}
		sched = existing
		return tx.Commit()
	})
	if err != nil {
		if connect.CodeOf(err) != connect.CodeUnknown {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("schedule %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.InfoContext(ctx, "schedule enabled set", "id", sched.ID, "enabled", sched.Enabled)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "updated", Type: "schedule", ID: sched.ID}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.SetScheduleEnabledResponse{Schedule: sched.Proto()}, nil
}
