package apiserver

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/authscope"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestCreateSchedule(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Name:      "nightly",
		Workspace: "test-workspace",
		Runner:    "test-runner",
		Namespace: "reviewbot",
		Instructions: []*xagentv1.Instruction{
			{Text: "bump deps", Url: "https://example.com/deps"},
		},
		CronExpr:    "0 9 * * *",
		Timezone:    "America/Toronto",
		Enabled:     proto.Bool(true),
		AutoArchive: durationpb.New(time.Hour),
	})
	assert.NilError(t, err)

	// An enabled schedule gets a first fire time computed server-side.
	assert.Assert(t, resp.Schedule.NextRunAt != nil)

	expected := &xagentv1.Schedule{
		Id:        resp.Schedule.Id,
		Name:      "nightly",
		Workspace: "test-workspace",
		Runner:    "test-runner",
		Namespace: "reviewbot",
		Instructions: []*xagentv1.Instruction{
			{Text: "bump deps", Url: "https://example.com/deps"},
		},
		CronExpr:    "0 9 * * *",
		Timezone:    "America/Toronto",
		Enabled:     true,
		AutoArchive: durationpb.New(time.Hour),
		CreatedBy:   org.UserID,
		NextRunAt:   resp.Schedule.NextRunAt, // nondeterministic (computed from now)
		CreatedAt:   resp.Schedule.CreatedAt,
		UpdatedAt:   resp.Schedule.UpdatedAt,
	}
	assert.DeepEqual(t, resp.Schedule, expected, protocmp.Transform())

	// It round-trips through Get. created_at/updated_at are ignored: the create
	// response carries the in-memory time at full precision, while the DB read-back
	// is truncated to Postgres's microsecond resolution.
	getResp, err := srv.GetSchedule(ctx, &xagentv1.GetScheduleRequest{Id: resp.Schedule.Id})
	assert.NilError(t, err)
	assert.DeepEqual(t, getResp.Schedule, resp.Schedule, protocmp.Transform(),
		protocmp.IgnoreFields(&xagentv1.Schedule{}, "created_at", "updated_at"))
}

func TestCreateSchedule_DisabledHasNoNextRun(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace",
		Runner:    "test-runner",
		CronExpr:  "0 9 * * *",
		Enabled:   proto.Bool(false),
	})
	assert.NilError(t, err)

	// A disabled schedule stays out of the claim query: no next fire time.
	assert.Assert(t, resp.Schedule.NextRunAt == nil)
	assert.Assert(t, !resp.Schedule.Enabled)
}

func TestCreateSchedule_EnabledDefaultsTrue(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	// enabled is presence-tracked: omitting it defaults to true (active), so the
	// schedule fires without a second enable call.
	resp, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace",
		Runner:    "test-runner",
		CronExpr:  "0 9 * * *",
	})
	assert.NilError(t, err)
	assert.Assert(t, resp.Schedule.Enabled)
	assert.Assert(t, resp.Schedule.NextRunAt != nil)
}

func TestCreateSchedule_DefaultTimezone(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	// An empty timezone defaults to UTC.
	resp, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace",
		Runner:    "test-runner",
		CronExpr:  "0 9 * * *",
	})
	assert.NilError(t, err)
	assert.Equal(t, resp.Schedule.Timezone, "UTC")
}

func TestCreateSchedule_InvalidCron(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	_, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace",
		Runner:    "test-runner",
		CronExpr:  "not a cron",
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeInvalidArgument)

	// The invalid schedule was never stored.
	list, err := srv.ListSchedules(ctx, &xagentv1.ListSchedulesRequest{})
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(list.Schedules, 0))
}

func TestCreateSchedule_InvalidTimezone(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	_, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace",
		Runner:    "test-runner",
		CronExpr:  "0 9 * * *",
		Timezone:  "Mars/Phobos",
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeInvalidArgument)
}

func TestCreateSchedule_BadWorkspace(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	_, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "fake-workspace",
		Runner:    "test-runner",
		CronExpr:  "0 9 * * *",
	})
	assert.ErrorContains(t, err, "not found")
}

func TestCreateSchedule_RequiresCreateScope(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})

	// A member holding only task-read (org membership, no create scope) is denied.
	readOnly := scopedCtx(t, org, authscope.Scopes{authscope.New(authscope.OpTaskRead)})
	_, err := srv.CreateSchedule(readOnly, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *",
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	// A caller holding task-create for the target (workspace, runner) may create.
	creator := scopedCtx(t, org, authscope.Scopes{authscope.New(authscope.OpTaskCreate,
		authscope.WithTaskWorkspace("test-workspace"),
		authscope.WithTaskRunner("test-runner"),
		authscope.WithTaskArchived(false),
	)})
	_, err = srv.CreateSchedule(creator, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *",
	})
	assert.NilError(t, err)
}

func TestListSchedules(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	_, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{Name: "a", Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *"})
	assert.NilError(t, err)
	_, err = srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{Name: "b", Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 10 * * *"})
	assert.NilError(t, err)

	list, err := srv.ListSchedules(ctx, &xagentv1.ListSchedulesRequest{})
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(list.Schedules, 2))

	// List is org-scoped: another org sees none.
	other := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	otherList, err := srv.ListSchedules(createCtx(t, other), &xagentv1.ListSchedulesRequest{})
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(otherList.Schedules, 0))
}

func TestGetSchedule_Permissions(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)

	resp, err := srv.CreateSchedule(ctxA, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *",
	})
	assert.NilError(t, err)

	// Its own org reads it; another org gets not-found (org-scoped).
	_, errA := srv.GetSchedule(ctxA, &xagentv1.GetScheduleRequest{Id: resp.Schedule.Id})
	_, errB := srv.GetSchedule(ctxB, &xagentv1.GetScheduleRequest{Id: resp.Schedule.Id})
	assert.NilError(t, errA)
	assert.ErrorContains(t, errB, "not found")
}

// A member holding only task-read (no create scope) may still list, get, and
// delete — the read/delete surface is gated on org membership, not create scope.
func TestSchedule_ReadDeleteNeedOnlyMembership(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})

	// Admin sets up a schedule; a read-only member operates on it.
	created, err := srv.CreateSchedule(createCtx(t, org), &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *",
	})
	assert.NilError(t, err)
	member := scopedCtx(t, org, authscope.Scopes{authscope.New(authscope.OpTaskRead)})

	_, err = srv.GetSchedule(member, &xagentv1.GetScheduleRequest{Id: created.Schedule.Id})
	assert.NilError(t, err)
	list, err := srv.ListSchedules(member, &xagentv1.ListSchedulesRequest{})
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(list.Schedules, 1))
	_, err = srv.DeleteSchedule(member, &xagentv1.DeleteScheduleRequest{Id: created.Schedule.Id})
	assert.NilError(t, err)

	_, err = srv.GetSchedule(member, &xagentv1.GetScheduleRequest{Id: created.Schedule.Id})
	assert.ErrorContains(t, err, "not found")
}

func TestSetScheduleEnabled(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	// Start disabled — no next fire time.
	created, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *", Enabled: proto.Bool(false),
	})
	assert.NilError(t, err)
	assert.Assert(t, created.Schedule.NextRunAt == nil)

	// Enabling recomputes next_run_at from now.
	enabled, err := srv.SetScheduleEnabled(ctx, &xagentv1.SetScheduleEnabledRequest{Id: created.Schedule.Id, Enabled: true})
	assert.NilError(t, err)
	assert.Assert(t, enabled.Schedule.Enabled)
	assert.Assert(t, enabled.Schedule.NextRunAt != nil)

	// Disabling clears it so the claim query skips the row.
	disabled, err := srv.SetScheduleEnabled(ctx, &xagentv1.SetScheduleEnabledRequest{Id: created.Schedule.Id, Enabled: false})
	assert.NilError(t, err)
	assert.Assert(t, !disabled.Schedule.Enabled)
	assert.Assert(t, disabled.Schedule.NextRunAt == nil)
}

func TestDeleteSchedule(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	created, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *",
	})
	assert.NilError(t, err)

	_, err = srv.DeleteSchedule(ctx, &xagentv1.DeleteScheduleRequest{Id: created.Schedule.Id})
	assert.NilError(t, err)

	_, err = srv.GetSchedule(ctx, &xagentv1.GetScheduleRequest{Id: created.Schedule.Id})
	assert.ErrorContains(t, err, "not found")
}

func TestUpdateSchedule(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	created, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Name: "original", Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *", Timezone: "UTC", Enabled: proto.Bool(true),
	})
	assert.NilError(t, err)
	firstNext := created.Schedule.NextRunAt.AsTime()

	// Rename and move the spec to a new time; next_run_at recomputes.
	updated, err := srv.UpdateSchedule(ctx, &xagentv1.UpdateScheduleRequest{
		Id: created.Schedule.Id, Name: "renamed", Workspace: "test-workspace", Runner: "test-runner", CronExpr: "30 2 * * *", Timezone: "UTC",
	})
	assert.NilError(t, err)
	assert.Equal(t, updated.Schedule.Name, "renamed")
	assert.Equal(t, updated.Schedule.CronExpr, "30 2 * * *")
	assert.Assert(t, updated.Schedule.NextRunAt != nil)
	assert.Assert(t, !updated.Schedule.NextRunAt.AsTime().Equal(firstNext))

	// Persisted.
	getResp, err := srv.GetSchedule(ctx, &xagentv1.GetScheduleRequest{Id: created.Schedule.Id})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Schedule.Name, "renamed")
	assert.Equal(t, getResp.Schedule.CronExpr, "30 2 * * *")
}

func TestUpdateSchedule_InvalidCron(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	created, err := srv.CreateSchedule(ctx, &xagentv1.CreateScheduleRequest{
		Workspace: "test-workspace", Runner: "test-runner", CronExpr: "0 9 * * *", Timezone: "UTC",
	})
	assert.NilError(t, err)

	_, err = srv.UpdateSchedule(ctx, &xagentv1.UpdateScheduleRequest{
		Id: created.Schedule.Id, Workspace: "test-workspace", Runner: "test-runner", CronExpr: "nonsense",
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeInvalidArgument)

	// The bad update never landed: the stored spec is unchanged.
	getResp, err := srv.GetSchedule(ctx, &xagentv1.GetScheduleRequest{Id: created.Schedule.Id})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Schedule.CronExpr, "0 9 * * *")
}
