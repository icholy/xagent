package server

import (
	"context"
	"fmt"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
)

func TestCreateEvent(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

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
	ctx := context.Background()
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

func TestListEvents(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()
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
	ctx := context.Background()

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
	ctx := context.Background()
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

func TestAddEventTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

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

func TestRemoveEventTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

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

func TestListEventTasks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

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
	ctx := context.Background()

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

func TestProcessEvent(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

	// Create two tasks with links to the same URL with notify=true
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

	// Create links with notify=true
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task1.Task.Id,
		Url:       "https://github.com/example/repo/pull/123",
		Relevance: "PR to monitor",
		Notify:    true,
	})
	assert.NilError(t, err)

	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task2.Task.Id,
		Url:       "https://github.com/example/repo/pull/123",
		Relevance: "PR to monitor",
		Notify:    true,
	})
	assert.NilError(t, err)

	// Create an event with matching URL
	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "PR comment added",
		Data:        `{"comment": "Please review"}`,
		Url:         "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	processResp, err := srv.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(processResp.TaskIds), 2)

	// Verify both tasks received the event
	events1, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{
		TaskId: task1.Task.Id,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(events1.Events), 1)
	assert.Equal(t, events1.Events[0].Id, eventResp.Event.Id)

	events2, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{
		TaskId: task2.Task.Id,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(events2.Events), 1)
	assert.Equal(t, events2.Events[0].Id, eventResp.Event.Id)

	// Verify both tasks were set to restarting status
	getTask1, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: task1.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getTask1.Task.Status, "restarting")

	getTask2, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: task2.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getTask2.Task.Status, "restarting")
}

func TestProcessEventWithoutURL(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

	// Create an event without URL
	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Event without URL",
		Data:        `{}`,
	})
	assert.NilError(t, err)

	// Act
	processResp, err := srv.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})

	// Assert - should succeed but route to no tasks
	assert.NilError(t, err)
	assert.Equal(t, len(processResp.TaskIds), 0)
}

func TestProcessEventWithNoMatchingLinks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

	task, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Create link with different URL
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task.Task.Id,
		Url:       "https://github.com/example/repo/pull/456",
		Relevance: "Different PR",
		Notify:    true,
	})
	assert.NilError(t, err)

	// Create event with different URL
	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "Different PR event",
		Data:        `{}`,
		Url:         "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	processResp, err := srv.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})

	// Assert - should route to no tasks
	assert.NilError(t, err)
	assert.Equal(t, len(processResp.TaskIds), 0)
}

func TestProcessEventWithNotifyFalse(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

	task, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Create link with notify=false
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task.Task.Id,
		Url:       "https://github.com/example/repo/pull/123",
		Relevance: "Reference only",
		Notify:    false,
	})
	assert.NilError(t, err)

	// Create event with matching URL
	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "PR event",
		Data:        `{}`,
		Url:         "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	processResp, err := srv.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})

	// Assert - should not route to task since notify=false
	assert.NilError(t, err)
	assert.Equal(t, len(processResp.TaskIds), 0)
}

func TestProcessEventDeduplicatesTasks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

	task, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Create two links from same task to same URL
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task.Task.Id,
		Url:       "https://github.com/example/repo/pull/123",
		Relevance: "Link 1",
		Notify:    true,
	})
	assert.NilError(t, err)

	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task.Task.Id,
		Url:       "https://github.com/example/repo/pull/123",
		Relevance: "Link 2",
		Notify:    true,
	})
	assert.NilError(t, err)

	// Create event with matching URL
	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "PR event",
		Data:        `{}`,
		Url:         "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	processResp, err := srv.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})

	// Assert - should only route to task once (deduplicated)
	assert.NilError(t, err)
	assert.Equal(t, len(processResp.TaskIds), 1)
	assert.Equal(t, processResp.TaskIds[0], task.Task.Id)
}

func TestProcessEventSkipsArchivedTasks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

	// Create two tasks with links to the same URL with notify=true
	activeTask, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Active Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	archivedTask, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Archived Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Archive the second task
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:     archivedTask.Task.Id,
		Status: "archived",
	})
	assert.NilError(t, err)

	// Create links with notify=true for both tasks
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    activeTask.Task.Id,
		Url:       "https://github.com/example/repo/pull/123",
		Relevance: "PR to monitor",
		Notify:    true,
	})
	assert.NilError(t, err)

	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    archivedTask.Task.Id,
		Url:       "https://github.com/example/repo/pull/123",
		Relevance: "PR to monitor",
		Notify:    true,
	})
	assert.NilError(t, err)

	// Create an event with matching URL
	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "PR comment added",
		Data:        `{"comment": "Please review"}`,
		Url:         "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	processResp, err := srv.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})

	// Assert - should only route to the active task, not the archived one
	assert.NilError(t, err)
	assert.Equal(t, len(processResp.TaskIds), 1)
	assert.Equal(t, processResp.TaskIds[0], activeTask.Task.Id)

	// Verify active task received the event and was set to restarting
	events1, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{
		TaskId: activeTask.Task.Id,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(events1.Events), 1)

	getActiveTask, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: activeTask.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getActiveTask.Task.Status, "restarting")

	// Verify archived task did NOT receive the event and remains archived
	events2, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{
		TaskId: archivedTask.Task.Id,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(events2.Events), 0)

	getArchivedTask, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: archivedTask.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getArchivedTask.Task.Status, "archived")
}
