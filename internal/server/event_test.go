package server

import (
	"fmt"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
)

func TestCreateEvent(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

	// Act
	resp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "PR comment added",
		Data:        `{"comment": "LGTM"}`,
		Url:         "https://github.com/example/repo/pull/123",
	})

	// Assert
	assert.NilError(t, err)
	expected := &xagentv1.Event{
		Id:          resp.Event.Id,
		Description: "PR comment added",
		Data:        `{"comment": "LGTM"}`,
		Url:         "https://github.com/example/repo/pull/123",
		CreatedAt:   resp.Event.CreatedAt,
	}
	assert.DeepEqual(t, resp.Event, expected, protocmp.Transform())
}

func TestGetEvent(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
	createResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Issue updated",
		Data:        `{"status": "closed"}`,
		Url:         "https://github.com/example/repo/issues/42",
	})
	assert.NilError(t, err)

	// Act
	getResp, err := srv.GetEvent(ctx, &xagentv1.GetEventRequest{
		Id: createResp.Event.Id,
	})

	// Assert
	assert.NilError(t, err)
	expected := &xagentv1.Event{
		Id:          createResp.Event.Id,
		Description: "Issue updated",
		Data:        `{"status": "closed"}`,
		Url:         "https://github.com/example/repo/issues/42",
		CreatedAt:   getResp.Event.CreatedAt,
	}
	assert.DeepEqual(t, getResp.Event, expected, protocmp.Transform())
}

func TestGetEvent_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
	createResp, err := srv.CreateEvent(userA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.GetEvent(userB, &xagentv1.GetEventRequest{
		Id: createResp.Event.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListEvents(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
	_, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 1",
		Data:        `{"test": "data1"}`,
	})
	assert.NilError(t, err)
	_, err = srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 2",
		Data:        `{"test": "data2"}`,
	})
	assert.NilError(t, err)

	// Act - uses default limit (100)
	resp, err := srv.ListEvents(ctx, &xagentv1.ListEventsRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Events), 2)
	// Events are ordered by created_at DESC (newest first)
	assert.Equal(t, resp.Events[0].Description, "Event 2")
	assert.Equal(t, resp.Events[1].Description, "Event 1")
}

func TestListEventsWithLimit(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

	// Create 5 events
	for i := range 5 {
		_, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
			Description: fmt.Sprintf("Event %d", i+1),
			Data:        `{}`,
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
	assert.Equal(t, resp.Events[0].Description, "Event 5")
	assert.Equal(t, resp.Events[1].Description, "Event 4")
}

func TestDeleteEvent(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
	createResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event to Delete",
		Data:        `{}`,
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
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
	createResp, err := srv.CreateEvent(userA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteEvent(userB, &xagentv1.DeleteEventRequest{
		Id: createResp.Event.Id,
	})

	// Assert - delete should silently fail (no error, but event still exists)
	assert.NilError(t, err)
	// Verify event still exists for user A
	_, err = srv.GetEvent(userA, &xagentv1.GetEventRequest{Id: createResp.Event.Id})
	assert.NilError(t, err)
}

func TestAddEventTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Test Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})

	// Assert
	assert.NilError(t, err)

	// Verify the association
	listResp, err := srv.ListEventTasks(ctx, &xagentv1.ListEventTasksRequest{
		EventId: eventResp.Event.Id,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.TaskIds), 1)
	assert.Equal(t, listResp.TaskIds[0], taskResp.Task.Id)
}

func TestAddEventTask_Permissions_Task(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")

	taskResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	eventResp, err := srv.CreateEvent(userB, &xagentv1.CreateEventRequest{
		Description: "User B's Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.AddEventTask(userB, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestAddEventTask_Permissions_Event(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")

	taskResp, err := srv.CreateTask(userB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	eventResp, err := srv.CreateEvent(userA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.AddEventTask(userB, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestRemoveEventTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Test Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.RemoveEventTask(ctx, &xagentv1.RemoveEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})

	// Assert
	assert.NilError(t, err)

	// Verify the association is removed
	listResp, err := srv.ListEventTasks(ctx, &xagentv1.ListEventTasksRequest{
		EventId: eventResp.Event.Id,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.TaskIds), 0)
}

func TestRemoveEventTask_Permissions_Task(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")

	taskResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	eventResp, err := srv.CreateEvent(userA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	_, err = srv.AddEventTask(userA, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.RemoveEventTask(userB, &xagentv1.RemoveEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestRemoveEventTask_Permissions_Event(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")

	taskResp, err := srv.CreateTask(userB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	eventResp, err := srv.CreateEvent(userA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	// User A links event to user B's task (user A owns event, user B owns task - neither can do this alone now)
	// Instead: both users create their own, then user B tries to remove user A's event from their task
	eventRespB, err := srv.CreateEvent(userB, &xagentv1.CreateEventRequest{
		Description: "User B's Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	_, err = srv.AddEventTask(userB, &xagentv1.AddEventTaskRequest{
		EventId: eventRespB.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	// Act - User B tries to remove User A's event (which isn't linked, but tests ownership check)
	_, err = srv.RemoveEventTask(userB, &xagentv1.RemoveEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListEventTasks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

	task1, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 1",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	task2, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 2",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Test Event",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  task1.Task.Id,
	})
	assert.NilError(t, err)

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  task2.Task.Id,
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListEventTasks(ctx, &xagentv1.ListEventTasksRequest{
		EventId: eventResp.Event.Id,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.TaskIds), 2)
}

func TestListEventsByTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	event1, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 1",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	event2, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event 2",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: event1.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: event2.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{
		TaskId: taskResp.Task.Id,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Events), 2)
	// Events are ordered by created_at DESC (newest first)
	assert.Equal(t, resp.Events[0].Description, "Event 2")
	assert.Equal(t, resp.Events[1].Description, "Event 1")
}
