package runner

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/runner/backend"
	dockerbackend "github.com/icholy/xagent/internal/runner/backend/docker"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/dockerx"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

func TestRunnerStart(t *testing.T) {
	abs, err := filepath.Abs("../../prebuilt")
	assert.NilError(t, err)
	t.Setenv("XAGENT_PREBUILT_DIR", abs)

	task := &model.Task{
		ID:        1,
		Name:      "test-task",
		Runner:    "test-runner",
		Workspace: "test",
		Status:    model.TaskStatusPending,
		Command:   model.TaskCommandStart,
		Version:   1,
	}

	// Create mock client. The driver and the injected MCP server now connect to
	// this server directly (over the host network), so it must answer the
	// driver's started/stopped events and the agent's get_my_task
	// (GetTaskDetails).
	mock := &xagentclient.ClientMock{
		CreateTaskTokenFunc: func(_ context.Context, req *xagentv1.CreateTaskTokenRequest) (*xagentv1.CreateTaskTokenResponse, error) {
			return &xagentv1.CreateTaskTokenResponse{Token: "test-token"}, nil
		},
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
		GetTaskDetailsFunc: func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: task.Proto("")}, nil
		},
	}

	// Create httptest server with the mock
	_, handler := xagentv1connect.NewXAgentServiceHandler(mock)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Create runner. The agent connects to the C2 directly over the network, so
	// the container shares the host network namespace (NetworkMode "host") to
	// reach the httptest server on 127.0.0.1, and ServerURL is the same URL.
	client := xagentclient.New(xagentclient.Options{BaseURL: ts.URL})
	be, err := dockerbackend.New(dockerbackend.Options{RunnerID: "test-runner"})
	assert.NilError(t, err)
	r, err := New(Options{
		Client:    client,
		Backend:   be,
		ServerURL: ts.URL,
		Queue:     NewEventQueue(EventQueueOptions{Client: client, Log: slog.Default()}),
		Workspaces: &workspace.Config{
			Workspaces: map[string]workspace.Workspace{
				"test": {
					Container: workspace.Container{
						Image:       "alpine:latest",
						NetworkMode: "host",
					},
					Agent: workspace.Agent{
						Type: "dummy",
						Dummy: &workspace.DummyConfig{
							Sleep: 1,
							ToolCalls: []agent.DummyToolCall{
								{Server: "xagent", Name: "get_my_task"},
							},
						},
					},
				},
			},
		},
		Concurrency: 1,
		RunnerID:    "test-runner",
	})
	assert.NilError(t, err)
	t.Cleanup(func() { r.Close() })

	docker, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	assert.NilError(t, err)
	defer docker.Close()

	// Remove any leftover container from a previous aborted run.
	_ = docker.ContainerRemove(t.Context(), "xagent-1", container.RemoveOptions{Force: true})

	// Start a task
	err = r.Start(t.Context(), task)
	assert.NilError(t, err)

	// Wait for the container to exit
	err = dockerx.ContainerWait(t.Context(), docker, "xagent-1", container.WaitConditionNotRunning)
	assert.NilError(t, err)

	// Remove the container
	err = docker.ContainerRemove(t.Context(), "xagent-1", container.RemoveOptions{})
	assert.NilError(t, err)

	// Verify get_my_task was called
	assert.Equal(t, len(mock.GetTaskDetailsCalls()), 1)

	// Verify the driver reported its own lifecycle: started, then stopped.
	var events []string
	for _, call := range mock.SubmitRunnerEventsCalls() {
		for _, e := range call.SubmitRunnerEventsRequest.Events {
			events = append(events, e.Event)
		}
	}
	assert.DeepEqual(t, events, []string{"started", "stopped"})
}

// submitted drains the queue through the mock client and returns the events
// it delivered.
func submitted(t *testing.T, mock *xagentclient.ClientMock, queue *EventQueue) []*xagentv1.RunnerEvent {
	t.Helper()
	assert.NilError(t, queue.Drain(t.Context()))
	var events []*xagentv1.RunnerEvent
	for _, call := range mock.SubmitRunnerEventsCalls() {
		events = append(events, call.SubmitRunnerEventsRequest.Events...)
	}
	return events
}

func TestRunnerPoll_StopWithoutSandbox(t *testing.T) {
	t.Parallel()
	// Arrange
	task := &model.Task{
		ID:        7,
		Runner:    "test-runner",
		Workspace: "test",
		Status:    model.TaskStatusCancelling,
		Command:   model.TaskCommandStop,
		Version:   3,
	}
	mock := &xagentclient.ClientMock{
		ListRunnerTasksFunc: func(_ context.Context, _ *xagentv1.ListRunnerTasksRequest) (*xagentv1.ListRunnerTasksResponse, error) {
			return &xagentv1.ListRunnerTasksResponse{Tasks: []*xagentv1.Task{task.Proto("")}}, nil
		},
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	be := &backend.BackendMock{
		StopFunc: func(_ context.Context, taskID int64) (bool, error) {
			return false, nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Poll(t.Context())
	assert.NilError(t, err)

	// Assert - no sandbox was signalled, so the runner emits "stopped" itself
	assert.Equal(t, len(be.StopCalls()), 1)
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "stopped")
	assert.Equal(t, events[0].TaskId, int64(7))
	assert.Equal(t, events[0].Version, int64(3))
}

func TestRunnerPoll_StopSignalled(t *testing.T) {
	t.Parallel()
	// Arrange
	task := &model.Task{
		ID:        7,
		Runner:    "test-runner",
		Workspace: "test",
		Status:    model.TaskStatusCancelling,
		Command:   model.TaskCommandStop,
		Version:   3,
	}
	mock := &xagentclient.ClientMock{
		ListRunnerTasksFunc: func(_ context.Context, _ *xagentv1.ListRunnerTasksRequest) (*xagentv1.ListRunnerTasksResponse, error) {
			return &xagentv1.ListRunnerTasksResponse{Tasks: []*xagentv1.Task{task.Proto("")}}, nil
		},
	}
	be := &backend.BackendMock{
		StopFunc: func(_ context.Context, taskID int64) (bool, error) {
			return true, nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Poll(t.Context())
	assert.NilError(t, err)

	// Assert - the driver was signalled and owns the terminal report
	assert.Equal(t, len(be.StopCalls()), 1)
	assert.Equal(t, queue.Len(), 0)
}

func TestRunnerMonitor(t *testing.T) {
	t.Parallel()
	// Arrange
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	be := &backend.BackendMock{
		WatchFunc: func(_ context.Context, handle func(backend.Exit)) error {
			handle(backend.Exit{TaskID: 1, ExitCode: 0})
			handle(backend.Exit{TaskID: 2, ExitCode: 137})
			return nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Monitor(t.Context())
	assert.NilError(t, err)

	// Assert - exit 0 means the driver already reported; non-zero means the
	// report was lost and the runner emits "failed" on its behalf
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "failed")
	assert.Equal(t, events[0].TaskId, int64(2))
	assert.Equal(t, events[0].Version, int64(0))
}

func TestRunnerReconcile(t *testing.T) {
	t.Parallel()
	// Arrange
	status := map[int64]xagentv1.TaskStatus{
		2: xagentv1.TaskStatus_RUNNING,
		3: xagentv1.TaskStatus_COMPLETED,
	}
	mock := &xagentclient.ClientMock{
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Status: status[req.Id]}}, nil
		},
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	be := &backend.BackendMock{
		ListFunc: func(_ context.Context) ([]backend.Sandbox, error) {
			return []backend.Sandbox{
				{TaskID: 1, State: backend.StateRunning},
				{TaskID: 2, State: backend.StateExited},
				{TaskID: 3, State: backend.StateExited},
			}, nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Reconcile(t.Context())
	assert.NilError(t, err)

	// Assert - only the exited sandbox whose task is still running means the
	// driver's report was lost
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "failed")
	assert.Equal(t, events[0].TaskId, int64(2))
}

func TestRunnerPrune(t *testing.T) {
	t.Parallel()
	// Arrange
	archived := map[int64]bool{1: true, 2: false}
	mock := &xagentclient.ClientMock{
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			if req.Id == 4 {
				return nil, connect.NewError(connect.CodeNotFound, errors.New("task not found"))
			}
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Archived: archived[req.Id]}}, nil
		},
	}
	be := &backend.BackendMock{
		ListFunc: func(_ context.Context) ([]backend.Sandbox, error) {
			return []backend.Sandbox{
				{TaskID: 1, State: backend.StateExited},  // archived -> removed
				{TaskID: 2, State: backend.StateExited},  // not archived -> kept
				{TaskID: 3, State: backend.StateRunning}, // running -> not considered
				{TaskID: 4, State: backend.StateExited},  // deleted -> removed
			}, nil
		},
		RemoveFunc: func(_ context.Context, taskID int64) error {
			return nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Prune(t.Context())
	assert.NilError(t, err)

	// Assert
	var removed []int64
	for _, call := range be.RemoveCalls() {
		removed = append(removed, call.TaskID)
	}
	assert.DeepEqual(t, removed, []int64{1, 4})
}
