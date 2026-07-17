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
		Instructions: instructionsFromProto(req.Instructions),
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
	s.publishScheduleChange(caller, "created", sched.ID)
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
		// Moving the spec re-aligns the schedule to a new grid; only then does
		// next_run_at need recomputing.
		specChanged := existing.CronExpr != req.CronExpr || existing.Timezone != timezone
		existing.Name = req.Name
		existing.Workspace = req.Workspace
		existing.Runner = req.Runner
		existing.Namespace = req.Namespace
		existing.Instructions = instructionsFromProto(req.Instructions)
		existing.CronExpr = req.CronExpr
		existing.Timezone = timezone
		existing.AutoArchive = req.AutoArchive.AsDuration()
		if err := existing.Validate(); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		// Only an enabled schedule tracks a next fire; recompute it from now when
		// the cron/timezone moved so the edit takes effect immediately. Enable and
		// disable stay in SetScheduleEnabled, so this never flips next_run_at on/off.
		if existing.Enabled && specChanged {
			next, err := existing.Next(time.Now())
			if err != nil {
				return connect.NewError(connect.CodeInvalidArgument, err)
			}
			existing.NextRunAt = &next
		}
		existing.Version++
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
	s.publishScheduleChange(caller, "updated", sched.ID)
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
	s.publishScheduleChange(caller, "deleted", req.Id)
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
		existing.Version++
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
	s.publishScheduleChange(caller, "updated", sched.ID)
	return &xagentv1.SetScheduleEnabledResponse{Schedule: sched.Proto()}, nil
}

// instructionsFromProto converts the request's instruction messages to the
// schedule's template DTOs (the [{text, url}] JSONB shape).
func instructionsFromProto(instructions []*xagentv1.Instruction) []model.ScheduleInstruction {
	out := make([]model.ScheduleInstruction, len(instructions))
	for i, inst := range instructions {
		out[i] = model.ScheduleInstruction{Text: inst.Text, URL: inst.Url}
	}
	return out
}

// publishScheduleChange emits the change notification for a schedule mutation so
// the Web UI refreshes. Schedules never wake a runner directly — the scheduler
// worker (L4) creates tasks — so no Runner is set.
func (s *Server) publishScheduleChange(caller *apiauth.UserInfo, action string, id int64) {
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: action, Type: "schedule", ID: id}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
}
