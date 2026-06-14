package apiserver

import (
	"context"
	"fmt"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
)

// orgWithWorkspace creates an org that has a runner/workspace pair, which is a
// prerequisite for creating the tasks that events are now scoped to.
func orgWithWorkspace(t *testing.T, srv *Server) *teststore.Org {
	t.Helper()
	return teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{
		Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}},
	})
}

// createTestTask creates a task to own the events under test.
func createTestTask(t *testing.T, srv *Server, ctx context.Context) int64 {
	t.Helper()
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	return resp.Task.Id
}

func TestCreateEvent(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)
	taskID := createTestTask(t, srv, ctx)

	// Act
	resp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "PR comment added",
		Data:        `{"comment": "LGTM"}`,
		Url:         "https://github.com/example/repo/pull/123",
		TaskId:      taskID,
	})

	// Assert
	assert.NilError(t, err)
	expected := &xagentv1.Event{
		Id:     resp.Event.Id,
		TaskId: taskID,
		Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
			Description: "PR comment added",
			Url:         "https://github.com/example/repo/pull/123",
			Data:        `{"comment": "LGTM"}`,
		}},
		CreatedAt: resp.Event.CreatedAt,
	}
	assert.DeepEqual(t, resp.Event, expected, protocmp.Transform())
}

func TestCreateEvent_TaskNotFound(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)

	// Act - reference a task that does not exist
	_, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Orphan event",
		Data:        `{}`,
		TaskId:      999999,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestGetEvent(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)
	taskID := createTestTask(t, srv, ctx)
	createResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Issue updated",
		Data:        `{"status": "closed"}`,
		Url:         "https://github.com/example/repo/issues/42",
		TaskId:      taskID,
	})
	assert.NilError(t, err)

	// Act
	getResp, err := srv.GetEvent(ctx, &xagentv1.GetEventRequest{
		Id: createResp.Event.Id,
	})

	// Assert
	assert.NilError(t, err)
	expected := &xagentv1.Event{
		Id:     createResp.Event.Id,
		TaskId: taskID,
		Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
			Description: "Issue updated",
			Url:         "https://github.com/example/repo/issues/42",
			Data:        `{"status": "closed"}`,
		}},
		CreatedAt: getResp.Event.CreatedAt,
	}
	assert.DeepEqual(t, getResp.Event, expected, protocmp.Transform())
}

func TestGetEvent_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := orgWithWorkspace(t, srv)
	ctxA := createCtx(t, orgA)
	orgB := orgWithWorkspace(t, srv)
	ctxB := createCtx(t, orgB)
	taskA := createTestTask(t, srv, ctxA)
	createResp, err := srv.CreateEvent(ctxA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
		TaskId:      taskA,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.GetEvent(ctxB, &xagentv1.GetEventRequest{
		Id: createResp.Event.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListEvents(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)
	taskID := createTestTask(t, srv, ctx)
	_, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 1",
		Data:        `{"test": "data1"}`,
		TaskId:      taskID,
	})
	assert.NilError(t, err)
	_, err = srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 2",
		Data:        `{"test": "data2"}`,
		TaskId:      taskID,
	})
	assert.NilError(t, err)

	// Act - uses default limit (100)
	resp, err := srv.ListEvents(ctx, &xagentv1.ListEventsRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Events), 2)
	// Events are ordered by created_at DESC (newest first)
	assert.Equal(t, resp.Events[0].GetExternal().Description, "Event 2")
	assert.Equal(t, resp.Events[1].GetExternal().Description, "Event 1")
}

func TestListEvents_ExternalOnly(t *testing.T) {
	t.Parallel()
	// Arrange - a task seeded with an instruction event, plus a link event from
	// CreateLink; both carry an org_id but are not org-feed rows.
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:         "Test Task",
		Runner:       "test-runner",
		Workspace:    "test-workspace",
		Instructions: []*xagentv1.Instruction{{Text: "do the thing"}},
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId: taskResp.Task.Id,
		Url:    "https://github.com/example/repo/pull/1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "PR comment",
		Data:        `{}`,
		TaskId:      taskResp.Task.Id,
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListEvents(ctx, &xagentv1.ListEventsRequest{})

	// Assert - the org feed returns only the external event; instruction and link
	// events do not leak into it.
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Events), 1)
	assert.Equal(t, resp.Events[0].GetExternal().Description, "PR comment")
}

func TestListEventsWithLimit(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)
	taskID := createTestTask(t, srv, ctx)

	// Create 5 events
	for i := range 5 {
		_, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
			Description: fmt.Sprintf("Event %d", i+1),
			Data:        `{}`,
			TaskId:      taskID,
		})
		assert.NilError(t, err)
	}

	// Act - Get only 2 most recent events
	resp, err := srv.ListEvents(ctx, &xagentv1.ListEventsRequest{
		Limit: 2,
	})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(resp.Events), 2)
	// Events are ordered by created_at DESC (newest first)
	assert.Equal(t, resp.Events[0].GetExternal().Description, "Event 5")
	assert.Equal(t, resp.Events[1].GetExternal().Description, "Event 4")
}

func TestListEvents_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := orgWithWorkspace(t, srv)
	ctxA := createCtx(t, orgA)
	orgB := orgWithWorkspace(t, srv)
	ctxB := createCtx(t, orgB)
	taskA := createTestTask(t, srv, ctxA)
	taskB := createTestTask(t, srv, ctxB)
	_, err := srv.CreateEvent(ctxA, &xagentv1.CreateEventRequest{
		Description: "User A's Event 1",
		Data:        `{}`,
		TaskId:      taskA,
	})
	assert.NilError(t, err)
	_, err = srv.CreateEvent(ctxA, &xagentv1.CreateEventRequest{
		Description: "User A's Event 2",
		Data:        `{}`,
		TaskId:      taskA,
	})
	assert.NilError(t, err)
	_, err = srv.CreateEvent(ctxB, &xagentv1.CreateEventRequest{
		Description: "User B's Event",
		Data:        `{}`,
		TaskId:      taskB,
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListEvents(ctxA, &xagentv1.ListEventsRequest{})
	assert.NilError(t, err)
	respB, err := srv.ListEvents(ctxB, &xagentv1.ListEventsRequest{})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Events), 2)
	assert.Equal(t, len(respB.Events), 1)
}

func TestDeleteEvent(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)
	taskID := createTestTask(t, srv, ctx)
	createResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event to Delete",
		Data:        `{}`,
		TaskId:      taskID,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteEvent(ctx, &xagentv1.DeleteEventRequest{
		Id: createResp.Event.Id,
	})

	// Assert
	assert.NilError(t, err)
	_, getErr := srv.GetEvent(ctx, &xagentv1.GetEventRequest{Id: createResp.Event.Id})
	assert.ErrorContains(t, getErr, "")
}

func TestDeleteEvent_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := orgWithWorkspace(t, srv)
	ctxA := createCtx(t, orgA)
	orgB := orgWithWorkspace(t, srv)
	ctxB := createCtx(t, orgB)
	taskA := createTestTask(t, srv, ctxA)
	createResp, err := srv.CreateEvent(ctxA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
		TaskId:      taskA,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteEvent(ctxB, &xagentv1.DeleteEventRequest{
		Id: createResp.Event.Id,
	})

	// Assert - delete should silently fail (no error, but event still exists)
	assert.NilError(t, err)
	// Verify event still exists for user A
	_, err = srv.GetEvent(ctxA, &xagentv1.GetEventRequest{Id: createResp.Event.Id})
	assert.NilError(t, err)
}

func TestListEventsByTask(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := orgWithWorkspace(t, srv)
	ctx := createCtx(t, org)
	taskID := createTestTask(t, srv, ctx)

	_, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 1",
		Data:        `{}`,
		TaskId:      taskID,
	})
	assert.NilError(t, err)

	_, err = srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 2",
		Data:        `{}`,
		TaskId:      taskID,
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{
		TaskId: taskID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Events), 2)
	// Events are ordered by created_at DESC (newest first)
	assert.Equal(t, resp.Events[0].GetExternal().Description, "Event 2")
	assert.Equal(t, resp.Events[1].GetExternal().Description, "Event 1")
}

func TestListEventsByTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := orgWithWorkspace(t, srv)
	ctxA := createCtx(t, orgA)
	orgB := orgWithWorkspace(t, srv)
	ctxB := createCtx(t, orgB)
	taskA := createTestTask(t, srv, ctxA)
	_, err := srv.CreateEvent(ctxA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
		TaskId:      taskA,
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListEventsByTask(ctxA, &xagentv1.ListEventsByTaskRequest{
		TaskId: taskA,
	})
	assert.NilError(t, err)
	respB, err := srv.ListEventsByTask(ctxB, &xagentv1.ListEventsByTaskRequest{
		TaskId: taskA,
	})
	assert.NilError(t, err)

	// Assert - org A sees its event; org B gets an empty list (blanket read skips
	// the row load; the list query is org-scoped).
	assert.Equal(t, len(respA.Events), 1)
	assert.Equal(t, len(respB.Events), 0)
}
