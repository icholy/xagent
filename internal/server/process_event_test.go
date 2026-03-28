package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestProcessEvent(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

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

	// Transition both tasks to running state so they can be restarted
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: task1.Task.Id, Event: "started", Version: task1.Task.Version},
			{TaskId: task2.Task.Id, Event: "started", Version: task2.Task.Version},
		},
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

	// Verify both tasks remain running (Start() doesn't interrupt running tasks)
	getTask1, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: task1.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getTask1.Task.Status, xagentv1.TaskStatus_RUNNING)
	assert.Equal(t, getTask1.Task.Command, xagentv1.TaskCommand_START)

	getTask2, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: task2.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getTask2.Task.Status, xagentv1.TaskStatus_RUNNING)
	assert.Equal(t, getTask2.Task.Command, xagentv1.TaskCommand_START)
}

func TestProcessEventWithoutURL(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

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
	ctx := createTestUser(t, srv)

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
	ctx := createTestUser(t, srv)

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
	ctx := createTestUser(t, srv)

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
	ctx := createTestUser(t, srv)

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

	// Transition both tasks to running state
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: activeTask.Task.Id, Event: "started", Version: activeTask.Task.Version},
			{TaskId: archivedTask.Task.Id, Event: "started", Version: archivedTask.Task.Version},
		},
	})
	assert.NilError(t, err)

	// Stop the archived task to transition running -> completed
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: archivedTask.Task.Id, Event: "stopped", Version: 0},
		},
	})
	assert.NilError(t, err)

	// Archive the second task
	_, err = srv.ArchiveTask(ctx, &xagentv1.ArchiveTaskRequest{
		Id: archivedTask.Task.Id,
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
	assert.Equal(t, getActiveTask.Task.Status, xagentv1.TaskStatus_RUNNING)
	assert.Equal(t, getActiveTask.Task.Command, xagentv1.TaskCommand_START)

	// Verify archived task did NOT receive the event and remains archived
	events2, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{
		TaskId: archivedTask.Task.Id,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(events2.Events), 0)

	getArchivedTask, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: archivedTask.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getArchivedTask.Task.Status, xagentv1.TaskStatus_COMPLETED)
	assert.Equal(t, getArchivedTask.Task.Archived, true)
}

func TestProcessEvent_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	eventResp, err := srv.CreateEvent(userA, &xagentv1.CreateEventRequest{
		Description: "User A's Event",
		Url:         "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.ProcessEvent(userB, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}
